package blob

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/fil-forge/libforge/capabilities"
	"github.com/fil-forge/libforge/capabilities/assert"
	"github.com/fil-forge/libforge/capabilities/blob"
	pdp_cap "github.com/fil-forge/libforge/capabilities/pdp"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/ipfs/go-cid"

	"github.com/fil-forge/piri/pkg/pdp"
	"github.com/fil-forge/piri/pkg/service/blobs"
	"github.com/fil-forge/piri/pkg/service/claims"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
)

type AcceptService interface {
	ID() principal.Signer
	PDP() pdp.PDP
	Blobs() blobs.Blobs
	Claims() claims.Claims
}

type AcceptRequest struct {
	Space did.DID
	Blob  blob.Blob
	// Put is the promise from blob/allocate that signals the upload completed.
	// Matches the `_put` field on blob.AcceptArguments.
	Put promise.AwaitOK
	// Cause is a link to the `blob/accept` or `blob/replica/transfer` invocation.
	Cause cid.Cid
}

type AcceptResponse struct {
	// Claim is the signed /assert/location invocation attesting that the blob is
	// retrievable at the given URL for the given space.
	Claim *invocation.Invocation
	// PDP is the queued /pdp/accept invocation whose receipt will be produced
	// once aggregation completes. Only set when PDP is enabled.
	PDP *invocation.Invocation
}

func Accept(ctx context.Context, s AcceptService, req *AcceptRequest) (resp *AcceptResponse, err error) {
	ctx, span := tracer.Start(ctx, "blob.accept")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	log := log.With("blob", req.Blob.Digest)
	log.Infof("%s %s", blob.AcceptCommand, req.Space)
	span.SetAttributes(
		attribute.Stringer("space.did", req.Space),
		attribute.Stringer("blob.digest", req.Blob.Digest),
		attribute.Int64("blob.size", int64(req.Blob.Size)),
	)

	var (
		loc          url.URL
		pdpAcceptInv *invocation.Invocation
	)
	if s.PDP() == nil {
		panic("pdp service required")
	} else {
		span.SetAttributes(attribute.Bool("pdp.enabled", true))
		// ensure the blob exists, else it cannot be accepted.
		found, err := s.PDP().API().Has(ctx, req.Blob.Digest)
		if err != nil {
			log.Errorw("finding piece for blob", "error", err)
			return nil, fmt.Errorf("finding piece for blob: %w", err)
		}
		if !found {
			log.Errorw("piece not found", "blob", req.Blob.Digest)
			return nil, fmt.Errorf("piece not found: %w", err)
		}
		// get a download url
		blobCID := cid.NewCidV1(cid.Raw, req.Blob.Digest)
		loc, err = s.PDP().API().ReadPieceURL(blobCID)
		if err != nil {
			log.Errorw("creating retrieval URL for blob", "error", err)
			return nil, fmt.Errorf("creating retrieval URL for blob: %w", err)
		}
		// submit the piece for aggregation
		if err := s.PDP().CommpCalculate().Enqueue(ctx, req.Blob.Digest); err != nil {
			log.Errorw("submitting piece for aggregation", "error", err)
			return nil, fmt.Errorf("submitting piece for aggregation: %w", err)
		}
		// generate the invocation that will complete when aggregation is complete and the piece is accepted
		pieceAccept, err := pdp_cap.Accept.Invoke(
			s.ID(),
			s.ID().DID(),
			&pdp_cap.AcceptArguments{
				Blob: req.Blob.Digest,
			},
			invocation.WithNoExpiration(),
		)
		if err != nil {
			log.Error("creating piece accept invocation", "error", err)
			return nil, fmt.Errorf("creating piece accept invocation: %w", err)
		}
		pdpAcceptInv = pieceAccept
	}

	byteRange := assert.Range{Offset: 0, Length: &req.Blob.Size}
	claim, err := assert.Location.Invoke(
		s.ID(),
		s.ID().DID(),
		&assert.LocationArguments{
			Space:    req.Space,
			Content:  req.Blob.Digest,
			Location: []capabilities.CborURL{capabilities.CborURL(loc)},
			Range:    &byteRange,
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
	}
	if pdpAcceptInv != nil {
		acc.PDPAccept = &acceptance.Promise{
			UcanAwait: acceptance.Await{
				Selector: ".out.ok",
				Link:     pdpAcceptInv.Link(),
			},
		}
	}
	err = s.Blobs().Acceptances().Put(ctx, acc)
	if err != nil {
		log.Errorw("putting acceptance for blob", "error", err)
		return nil, fmt.Errorf("putting acceptance for blob: %w", err)
	}

	err = s.Claims().Store().Put(ctx, claim)
	if err != nil {
		log.Errorw("putting location claim for blob", "error", err)
		return nil, fmt.Errorf("putting location claim for blob: %w", err)
	}

	err = s.Claims().Publisher().Publish(ctx, claim)
	if err != nil {
		log.Errorw("publishing location commitment", "error", err)
		return nil, fmt.Errorf("publishing location commitment: %w", err)
	}

	return &AcceptResponse{
		Claim: claim,
		PDP:   pdpAcceptInv,
	}, nil
}
