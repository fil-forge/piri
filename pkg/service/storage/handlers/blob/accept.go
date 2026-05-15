package blob

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/fil-forge/go-libstoracha/capabilities/assert"
	"github.com/fil-forge/go-libstoracha/capabilities/blob"
	pdp_cap "github.com/fil-forge/go-libstoracha/capabilities/pdp"
	"github.com/fil-forge/go-libstoracha/capabilities/types"
	"github.com/fil-forge/go-ucanto/core/delegation"
	"github.com/fil-forge/go-ucanto/core/invocation"
	"github.com/fil-forge/go-ucanto/did"
	"github.com/fil-forge/go-ucanto/principal"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	"github.com/multiformats/go-multihash"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/service/claims"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
)

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

// AcceptDeps is the dependency set populated by fx for the Accept handler.
type AcceptDeps struct {
	fx.In
	ID          principal.Signer
	Acceptances AcceptanceStore
	Pieces      PieceReader
	Commp       commp.Calculator
	Claims      claims.Claims
}

var (
	_ AcceptanceStore = (acceptancestore.AcceptanceStore)(nil)
	_ PieceReader     = (pdptypes.PieceAPI)(nil)
)

type AcceptRequest struct {
	Space did.DID
	Blob  types.Blob
	Put   blob.Promise
	// Cause is a link to the `blob/accept` or `blob/replica/transfer` invocation.
	Cause ipld.Link
}

type AcceptResponse struct {
	Claim delegation.Delegation
	PDP   invocation.Invocation
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
	log.Infof("%s %s", blob.AcceptAbility, req.Space)
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
	pdpAcceptInv, err := pdp_cap.Accept.Invoke(
		deps.ID,
		deps.ID,
		deps.ID.DID().String(),
		pdp_cap.AcceptCaveats{
			Blob: req.Blob.Digest,
		}, delegation.WithNoExpiration())
	if err != nil {
		log.Error("creating piece accept invocation", "error", err)
		return nil, fmt.Errorf("creating piece accept invocation: %w", err)
	}

	byteRange := assert.Range{Offset: 0, Length: &req.Blob.Size}
	claim, err := assert.Location.Delegate(
		deps.ID,
		req.Space,
		deps.ID.DID().String(),
		assert.LocationCaveats{
			Space:    req.Space,
			Content:  types.FromHash(req.Blob.Digest),
			Location: []url.URL{loc},
			Range:    &byteRange,
		},
		delegation.WithNoExpiration(),
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

	err = deps.Claims.Store().Put(ctx, claim)
	if err != nil {
		log.Errorw("putting location claim for blob", "error", err)
		return nil, fmt.Errorf("putting location claim for blob: %w", err)
	}

	err = deps.Claims.Publisher().Publish(ctx, claim)
	if err != nil {
		log.Errorw("publishing location commitment", "error", err)
		return nil, fmt.Errorf("publishing location commitment: %w", err)
	}

	return &AcceptResponse{
		Claim: claim,
		PDP:   pdpAcceptInv,
	}, nil
}
