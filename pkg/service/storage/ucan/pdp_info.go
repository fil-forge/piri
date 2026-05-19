package ucan

import (
	"bytes"
	"context"
	"fmt"

	logging "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multihash"
	fxlib "go.uber.org/fx"

	"github.com/fil-forge/libforge/capabilities/pdp"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/principal"

	piecepkg "github.com/fil-forge/piri/pkg/pdp/piece"
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

func NewPDPInfoHandler(deps PDPInfoDeps) Handler {
	return TypedHandler(
		pdp.Info,
		func(req *bindexec.Request[*pdp.InfoArguments], rsp *bindexec.Response[*pdp.InfoOK]) error {
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

			// TODO(forrest)[claude I think this TODO is don't verify)
			// TODO(forrest)[ucan1]: the receipt store is still go-ucanto-
			// typed. deps.Receipts.GetByRan returns a *receipt.Receipt
			// today; the call below won't compile until Phase 5a migrates
			// receiptstore to ucantone. The shape below is what it WILL
			// look like: rcpt.Out().Unpack() yields ok/err CBOR bytes,
			// decoded via pdp.AcceptOK{}.UnmarshalCBOR.
			rcpt, err := deps.Receipts.GetByRan(ctx, pdpAcceptInv.Link())
			if err != nil {
				// Receipt absent is expected while aggregation is in
				// flight — surface as a normal failure result, not a
				// transport error.
				log.Errorw("looking up /pdp/accept receipt", "error", err)
				return rsp.SetFailure(err)
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
			commpCid := piecepkg.MultihashToCommpCID(resolvedCommp)
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
