package pdp

import (
	"bytes"
	"context"
	"fmt"

	"github.com/fil-forge/ucantone/binding"
	"github.com/fil-forge/ucantone/server"
	logging "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multihash"
	fxlib "go.uber.org/fx"

	"github.com/fil-forge/libforge/commands/pdp"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/principal"

	libpiece "github.com/fil-forge/libforge/piece"
	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

var log = logging.Logger("storage/ucan")

// PieceResolver is the slice of the PDP piece API the pdp/info handler
// depends on.
type PieceResolver interface {
	ResolveToPiece(ctx context.Context, blob multihash.Multihash) (multihash.Multihash, bool, error)
	CalculateCommP(ctx context.Context, blob multihash.Multihash) (pdptypes.CalculateCommPResponse, error)
}

// PDPInfoDeps is the dependency set for the pdp/info UCAN method.
type PDPInfoDeps struct {
	fxlib.In
	ID       principal.Signer
	Receipts receiptstore.ReceiptStore
	Pieces   PieceResolver
}

var _ PieceResolver = (pdptypes.PieceAPI)(nil)

// PieceMismatchErrorName is the stable receipt-failure name when the
// resolved commp doesn't match the piece CID recorded in the /pdp/accept
// receipt — a hard invariant violation.
const PieceMismatchErrorName = "PieceMismatch"

func NewPDPInfoHandler(deps PDPInfoDeps) server.Route {
	return pdp.Info.Route(func(req *binding.Request[*pdp.InfoArguments], rsp *binding.Response[*pdp.InfoOK]) error {
		args := req.Task().Arguments()
		ctx := req.Context()

		// No subject check — the legacy /pdp/info handler is open by
		// design (any holder of the delegation can ask whether a blob
		// is aggregated yet).

		// Try to resolve the blob multihash to its commp.
		resolvedCommp, found, err := deps.Pieces.ResolveToPiece(ctx, args.Blob)
		if err != nil {
			log.Errorw("failed to resolve PDP api", "error", err)
			return fmt.Errorf("failed to resolve PDP API: %w", err)
		}
		if !found {
			// commp not computed yet — compute on demand
			commpResp, err := deps.Pieces.CalculateCommP(ctx, args.Blob)
			if err != nil {
				log.Errorw("failed to compute commp", "digest", args.Blob.String(), "error", err)
				return fmt.Errorf("failed to compute commp: %w", err)
			}
			return rsp.SetSuccess(&pdp.InfoOK{
				Piece:      commpResp.PieceCID,
				Aggregates: []pdp.InfoAcceptedAggregate{},
			})
		}

		// commp resolved — fetch the /pdp/accept receipt to learn the
		// aggregate and inclusion proof. We rebuild the /pdp/accept
		// invocation deterministically; its Link() is the key the
		// receipt store indexed under.
		pdpAcceptInv, err := pdp.Accept.Invoke(
			deps.ID, deps.ID.DID(),
			&pdp.AcceptArguments{Blob: args.Blob},
		)
		if err != nil {
			log.Errorw("building /pdp/accept invocation", "error", err)
			return fmt.Errorf("building /pdp/accept invocation: %w", err)
		}

		// The receipt is indexed under the /pdp/accept task link, and
		// rcpt.Out().Unpack() yields ok/err CBOR bytes decoded via
		// pdp.AcceptOK{}.UnmarshalCBOR.
		// TODO(forrest)[ucan1]: revisit how this lookup composes once the
		// receipt/acceptance stores are fully migrated — see #11.
		rcpt, err := deps.Receipts.GetByRan(ctx, pdpAcceptInv.Task().Link())
		if err != nil {
			// An error looking up the receipt is unexpected (internal),
			// not a named result the client should interpret — return it.
			log.Errorw("looking up /pdp/accept receipt", "error", err)
			return err
		}
		okBytes, errBytes := rcpt.Out().Unpack()
		if errBytes != nil {
			// /pdp/accept itself failed — propagate.
			return rsp.SetFailure(errors.New(
				"PDPAcceptFailed",
				"upstream /pdp/accept receipt is a failure",
			))
		}
		var acc pdp.AcceptOK
		if err := acc.UnmarshalCBOR(bytes.NewReader(okBytes)); err != nil {
			log.Errorw("decoding /pdp/accept ok", "error", err)
			return fmt.Errorf("decoding /pdp/accept ok: %w", err)
		}

		// Sanity check: the receipt's piece CID should match what we
		// resolved from the blob multihash.
		commpCid := libpiece.MultihashToCommpCID(resolvedCommp)
		if !acc.Piece.Equals(commpCid) {
			log.Errorw("piece CID mismatch",
				"expected", commpCid, "got", acc.Piece,
			)
			return rsp.SetFailure(errors.New(
				PieceMismatchErrorName,
				"resolved piece %s != receipt piece %s", commpCid, acc.Piece,
			))
		}

		return rsp.SetSuccess(&pdp.InfoOK{
			Piece: acc.Piece,
			Aggregates: []pdp.InfoAcceptedAggregate{
				{
					Aggregate:      acc.Aggregate,
					InclusionProof: acc.InclusionProof,
				},
			},
		})
	},
	)
}
