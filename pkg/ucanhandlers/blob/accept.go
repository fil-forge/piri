package blob

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/fil-forge/libforge/commands"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/fx"

	"github.com/fil-forge/libforge/commands/assert"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/commands/pdp"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"

	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/service/publisher"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
	"github.com/fil-forge/piri/pkg/ucanhandlers"
)

// InternalErrorName is the stable receipt-failure name for invariant
// violations the handler hits at runtime (e.g., a link type we expected
// to be a cidlink wasn't).
const InternalErrorName = "InternalError"

// AcceptDeps is the dependency set populated by fx for the Accept handler.
type AcceptDeps struct {
	fx.In
	ID          principal.Signer
	Acceptances AcceptanceStore
	Pieces      PieceReader
	Commp       commp.Calculator
	ClaimStore  invocationstore.InvocationStore
	Publisher   publisher.Publisher
}

// AcceptanceStore is the slice of acceptancestore.AcceptanceStore the
// Accept handler depends on.
type AcceptanceStore interface {
	Put(ctx context.Context, a acceptance.Acceptance) error
}

// PieceReader is the slice of the PDP piece API the Accept handler depends on.
type PieceReader interface {
	Has(ctx context.Context, digest multihash.Multihash) (bool, error)
	ReadPieceURL(blob cid.Cid) (url.URL, error)
}

var (
	_ AcceptanceStore = (acceptancestore.AcceptanceStore)(nil)
	_ PieceReader     = (pdptypes.PieceAPI)(nil)
)

func NewAcceptHandler(deps AcceptDeps) ucanhandlers.CapabilityHandler {
	return ucanhandlers.TypedHandler(
		blob.Accept,
		func(req *bindexec.Request[*blob.AcceptArguments], rsp *bindexec.Response[*blob.AcceptOK]) error {
			args := req.Task().Arguments()

			// /blob/accept is space-scoped: the invocation subject IS
			// the space being accepted into. Authorization is enforced
			// by the validator's proof chain, not by an issuer-equals-
			// service check.

			resp, err := Accept(req.Context(), deps, &AcceptRequest{
				Space: req.Task().Subject(),
				Blob: blob.Blob{
					Digest: args.Blob.Digest,
					Size:   args.Blob.Size,
				},
				// TODO(forrest)[ucan1]: need to double check this..
				Put:   args.Put,
				Cause: req.Invocation().Link(),
			})
			if err != nil {
				return err
			}

			// TODO(forrest)[ucan1]: ensure this is the correct way to attach the invocations
			if err := rsp.SetMetadata(container.New(container.WithInvocations(resp.Claim, resp.PDP))); err != nil {
				return fmt.Errorf("setting metadata on response: %w", err)
			}
			return rsp.SetSuccess(&blob.AcceptOK{Site: resp.Claim.Link()})
		},
	)
}

type AcceptRequest struct {
	Space did.DID
	Blob  blob.Blob
	Put   promise.AwaitOK
	// Cause is a link to the `blob/accept` or `blob/replica/transfer` invocation.
	Cause cid.Cid
}

type AcceptResponse struct {
	Claim ucan.Invocation
	PDP   ucan.Invocation
}

func Accept(ctx context.Context, deps AcceptDeps, req *AcceptRequest) (resp *AcceptResponse, err error) {
	ctx, span := tracer.Start(ctx, "blob.accept")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	log := log.With("blob", req.Blob.Digest)
	log.Infof("%s %s", blob.Accept.Command, req.Space)
	span.SetAttributes(
		attribute.Stringer("space.did", req.Space),
		attribute.Stringer("blob.digest", req.Blob.Digest),
		attribute.Int64("blob.size", int64(req.Blob.Size)),
	)

	// ensure the blob exists, else it cannot be accepted.
	found, err := deps.Pieces.Has(ctx, req.Blob.Digest)
	if err != nil {
		log.Errorw("finding piece for blob", "error", err)
		return nil, fmt.Errorf("finding piece for blob: %w", err)
	}
	if !found {
		log.Errorw("piece not found", "blob", req.Blob.Digest)
		return nil, fmt.Errorf("piece not found")
	}
	// get a download url
	blobCID := cid.NewCidV1(cid.Raw, req.Blob.Digest)
	loc, err := deps.Pieces.ReadPieceURL(blobCID)
	if err != nil {
		log.Errorw("creating retrieval URL for blob", "error", err)
		return nil, fmt.Errorf("creating retrieval URL for blob: %w", err)
	}
	// submit the piece for aggregation
	if err := deps.Commp.Enqueue(ctx, req.Blob.Digest); err != nil {
		log.Errorw("submitting piece for aggregation", "error", err)
		return nil, fmt.Errorf("submitting piece for aggregation: %w", err)
	}
	// generate the invocation that will complete when aggregation is complete and the piece is accepted
	pdpAcceptInv, err := pdp.Accept.Invoke(
		deps.ID,
		deps.ID.DID(),
		&pdp.AcceptArguments{
			Blob: req.Blob.Digest,
		},
		invocation.WithNoExpiration(),
	)
	if err != nil {
		log.Error("creating piece accept invocation", "error", err)
		return nil, fmt.Errorf("creating piece accept invocation: %w", err)
	}

	claim, err := assert.Location.Invoke(
		deps.ID,
		req.Space,
		&assert.LocationArguments{
			Space:    req.Space,
			Content:  req.Blob.Digest,
			Location: []commands.CborURL{commands.CborURL(loc)},
			Range:    &assert.Range{Start: 0, End: &req.Blob.Size},
		},
		invocation.WithNoExpiration(),
	)

	if err != nil {
		log.Errorw("creating location commitment", "error", err)
		return nil, fmt.Errorf("creating location commitment: %w", err)
	}

	acc := acceptance.Acceptance{
		Space: req.Space,
		Blob: acceptance.Blob{
			Digest: req.Blob.Digest,
			Size:   req.Blob.Size,
		},
		ExecutedAt: uint64(time.Now().Unix()),
		Cause:      req.Cause,
		PDPAccept: &acceptance.Promise{
			UcanAwait: acceptance.Await{
				Selector: ".out.ok",
				Link:     pdpAcceptInv.Link(),
			},
		},
	}
	err = deps.Acceptances.Put(ctx, acc)
	if err != nil {
		log.Errorw("putting acceptance for blob", "error", err)
		return nil, fmt.Errorf("putting acceptance for blob: %w", err)
	}

	err = deps.ClaimStore.Put(ctx, claim)
	if err != nil {
		log.Errorw("putting location claim for blob", "error", err)
		return nil, fmt.Errorf("putting location claim for blob: %w", err)
	}

	err = deps.Publisher.Publish(ctx, claim)
	if err != nil {
		log.Errorw("publishing location commitment", "error", err)
		return nil, fmt.Errorf("publishing location commitment: %w", err)
	}

	return &AcceptResponse{
		Claim: claim,
		PDP:   pdpAcceptInv,
	}, nil
}
