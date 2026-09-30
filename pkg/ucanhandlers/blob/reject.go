package blob

import (
	"context"
	"fmt"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/fx"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/digestutil"
	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/ucantone/binding"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/server"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
)

// RejectDeps is the dependency set populated by fx for the Reject handler.
type RejectDeps struct {
	fx.In
	ID          identity.Identity
	Allocations AllocationRemover
	Acceptances AcceptanceChecker
	Pieces      PieceRemover
	Pending     PendingAllocations
	Uploads     UploadDiscarder
}

// AcceptanceChecker is the slice of acceptancestore.AcceptanceStore the
// Reject handler depends on.
type AcceptanceChecker interface {
	Get(ctx context.Context, digest multihash.Multihash, space did.DID) (acceptance.Acceptance, error)
	ListSpaces(ctx context.Context, digest multihash.Multihash) ([]did.DID, error)
}

var _ AcceptanceChecker = (acceptancestore.AcceptanceStore)(nil)

func NewBlobRejectHandler(deps RejectDeps) server.Route {
	return blob.Reject.Route(func(req *binding.Request[*blob.RejectArguments], rsp *binding.Response[*blob.RejectOK]) error {
		args := req.Task().Arguments()

		// The route's middleware has already required an invocation subjected to
		// this provider and issued by someone it delegated to (pkg/ucanhandlers).

		var err error
		if link, ok := args.Allocation(); ok {
			err = RejectAllocation(req.Context(), deps, args.Space(), link)
		} else {
			digest, _ := args.Digest()
			err = Reject(req.Context(), deps, &RejectRequest{
				Space:  args.Space(),
				Digest: digest,
			})
		}
		if err != nil {
			var named errors.Named
			if errors.As(err, &named) {
				return rsp.SetFailure(named)
			}
			return err
		}

		return rsp.SetSuccess(&blob.RejectOK{})
	})
}

type RejectRequest struct {
	Space  did.DID
	Digest multihash.Multihash
}

// Reject retires a parked blob — the "don't accept" exit of the
// allocate→accept|reject lifecycle: it deletes the space's allocation for
// the digest and, when no space holds an allocation or acceptance
// afterward, queues the bytes for release. A blob THE INVOKING SPACE has
// accepted is refused with BlobAccepted — the space's acceptance carries
// claims and is released via /blob/remove. The guard is scoped to the
// invoking space, not the digest: another space's acceptance of the same
// bytes never blocks the reject — the space's allocation is dropped and the
// bytes are retained for whoever still claims them. Idempotent: rejecting
// an unknown or already-rejected blob succeeds.
func Reject(ctx context.Context, deps RejectDeps, req *RejectRequest) (err error) {
	ctx, span := tracer.Start(ctx, "blob.reject")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	log := log.With("blob", digestutil.Format(req.Digest))
	log.Infof("%s space: %s", blob.Reject.Command, req.Space)
	span.SetAttributes(
		attribute.Stringer("space.did", req.Space),
		attribute.Stringer("blob.digest", req.Digest),
	)

	// Reject operates strictly on parked blobs. An acceptance by THE
	// INVOKING SPACE means its bytes carry claims (and may be aggregated) —
	// that space must release its claim via /blob/remove instead. Another
	// space's acceptance is irrelevant: each space exits its own lifecycle
	// independently, and shared bytes are protected by the claim count
	// below, not by this guard.
	_, err = deps.Acceptances.Get(ctx, req.Digest, req.Space)
	if err == nil {
		return errors.New(blob.BlobAcceptedErrorName,
			"blob %s has been accepted by %s; release the claim via %s",
			digestutil.Format(req.Digest), req.Space, blob.Remove.Command)
	} else if !errors.Is(err, store.ErrNotFound) {
		log.Errorw("checking acceptance", "error", err)
		return fmt.Errorf("checking acceptance: %w", err)
	}

	if err := deps.Allocations.Delete(ctx, req.Digest, req.Space); err != nil {
		log.Errorw("deleting allocation", "error", err)
		return fmt.Errorf("deleting allocation: %w", err)
	}
	return removeIfUnclaimed(ctx, deps, req.Digest)
}

// RejectAllocation retires an allocation made without a digest, named by its
// `/blob/allocate` task. It drops the upload, any staged data and the pending
// record. Once the data was received, the upload also released its claim on
// (digest, space), and the bytes are queued for release when nothing else
// claims them. An allocation this upload has accepted is refused with
// BlobAccepted. Idempotent: rejecting an unknown or already-rejected
// allocation succeeds.
func RejectAllocation(ctx context.Context, deps RejectDeps, space did.DID, link cid.Cid) (err error) {
	ctx, span := tracer.Start(ctx, "blob.reject")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	log := log.With("allocation", link)
	span.SetAttributes(
		attribute.Stringer("space.did", space),
		attribute.Stringer("blob.allocation", link),
	)

	p, err := deps.Pending.GetPending(ctx, link)
	if errors.Is(err, store.ErrNotFound) {
		log.Infof("%s space: %s (unknown allocation)", blob.Reject.Command, space)
		return nil
	} else if err != nil {
		log.Errorw("getting pending allocation", "error", err)
		return fmt.Errorf("getting pending allocation: %w", err)
	}
	// Once the data was received its digest is known, and is what the blob is
	// logged by everywhere else.
	if len(p.Digest) > 0 {
		log = log.With("blob", digestutil.Format(p.Digest))
	}
	log.Infof("%s space: %s", blob.Reject.Command, space)
	// Another space's allocation is not this space's to reject; report it as
	// unknown, as the digest path does.
	if p.Space != space {
		return nil
	}
	if p.Accepted {
		return errors.New(blob.BlobAcceptedErrorName,
			"allocation %s has been accepted by %s; release the claim via %s",
			link, space, blob.Remove.Command)
	}

	digest, err := releasePending(ctx, deps.Pending, deps.Uploads, p)
	if err != nil {
		log.Errorw("releasing pending allocation", "error", err)
		return err
	}
	if digest == nil {
		return nil
	}
	return removeIfUnclaimed(ctx, deps, digest)
}

// removeIfUnclaimed queues the bytes of digest for release when no space holds
// an allocation or an acceptance of them.
func removeIfUnclaimed(ctx context.Context, deps RejectDeps, digest multihash.Multihash) error {
	log := log.With("blob", digestutil.Format(digest))

	// Bytes are released only when no space holds an allocation or an
	// acceptance — another space's in-flight upload or accepted copy of the
	// same content shares them.
	allocSpaces, err := deps.Allocations.ListSpaces(ctx, digest)
	if err != nil {
		log.Errorw("listing allocation spaces", "error", err)
		return fmt.Errorf("listing allocation spaces: %w", err)
	}
	acceptSpaces, err := deps.Acceptances.ListSpaces(ctx, digest)
	if err != nil {
		log.Errorw("listing acceptance spaces", "error", err)
		return fmt.Errorf("listing acceptance spaces: %w", err)
	}
	if len(allocSpaces) > 0 || len(acceptSpaces) > 0 {
		log.Infow("blob still claimed, retaining bytes",
			"allocations", len(allocSpaces), "acceptances", len(acceptSpaces))
		return nil
	}

	// Queue the byte release; the removal machinery re-verifies claims (and
	// pipeline state) before deleting, staying safe against a racing accept.
	if err := deps.Pieces.RemovePiece(ctx, digest); err != nil {
		log.Errorw("removing parked piece", "error", err)
		return fmt.Errorf("removing parked piece: %w", err)
	}
	return nil
}
