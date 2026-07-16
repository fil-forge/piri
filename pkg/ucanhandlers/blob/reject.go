package blob

import (
	"context"
	"fmt"

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

	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/ucanhandlers"
)

// BlobAcceptedErrorName is the stable receipt-failure name when reject is
// invoked for a blob that has been accepted — accepted blobs are released
// via /blob/remove, never rejected.
const BlobAcceptedErrorName = "BlobAccepted"

// RejectDeps is the dependency set populated by fx for the Reject handler.
type RejectDeps struct {
	fx.In
	ID          identity.Identity
	Allocations AllocationRemover
	Acceptances AcceptanceChecker
	Pieces      PieceRemover
}

// AcceptanceChecker is the slice of acceptancestore.AcceptanceStore the
// Reject handler depends on.
type AcceptanceChecker interface {
	Exists(ctx context.Context, digest multihash.Multihash) (bool, error)
}

var _ AcceptanceChecker = (acceptancestore.AcceptanceStore)(nil)

func NewBlobRejectHandler(deps RejectDeps) server.Route {
	return blob.Reject.Route(func(req *binding.Request[*blob.RejectArguments], rsp *binding.Response[*blob.RejectOK]) error {
		args := req.Task().Arguments()

		// The invocation subject must be this storage provider; the space
		// abandoning its upload travels in the arguments. Authorization that
		// the upload service may invoke /blob/reject is enforced by the
		// validator's proof chain (rooted at the provider).
		if err := ucanhandlers.RequireSubject(req, deps.ID.DID()); err != nil {
			return rsp.SetFailure(err)
		}

		if err := Reject(req.Context(), deps, &RejectRequest{
			Space:  args.Space,
			Digest: args.Digest,
		}); err != nil {
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
// the digest and, when no space holds an allocation afterward, queues the
// bytes for release. A blob with any acceptance is refused with
// BlobAccepted — accepted blobs carry claims and are released via
// /blob/remove. Idempotent: rejecting an unknown or already-rejected blob
// succeeds.
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

	// Reject operates strictly on parked blobs. An acceptance in ANY space
	// means the bytes carry claims (and may be aggregated) — the caller
	// must release its claim via /blob/remove instead.
	accepted, err := deps.Acceptances.Exists(ctx, req.Digest)
	if err != nil {
		log.Errorw("checking acceptance", "error", err)
		return fmt.Errorf("checking acceptance: %w", err)
	}
	if accepted {
		return errors.New(BlobAcceptedErrorName,
			"blob %s has been accepted; release the claim via %s",
			digestutil.Format(req.Digest), blob.Remove.Command)
	}

	if err := deps.Allocations.Delete(ctx, req.Digest, req.Space); err != nil {
		log.Errorw("deleting allocation", "error", err)
		return fmt.Errorf("deleting allocation: %w", err)
	}

	// Bytes are released only when no space holds an allocation — another
	// space's in-flight upload of the same content shares them.
	allocSpaces, err := deps.Allocations.ListSpaces(ctx, req.Digest)
	if err != nil {
		log.Errorw("listing allocation spaces", "error", err)
		return fmt.Errorf("listing allocation spaces: %w", err)
	}
	if len(allocSpaces) > 0 {
		log.Infow("blob still allocated in other spaces, retaining bytes",
			"allocations", len(allocSpaces))
		return nil
	}

	// Queue the byte release; the removal machinery re-verifies claims (and
	// pipeline state) before deleting, staying safe against a racing accept.
	if err := deps.Pieces.RemovePiece(ctx, req.Digest); err != nil {
		log.Errorw("removing parked piece", "error", err)
		return fmt.Errorf("removing parked piece: %w", err)
	}
	return nil
}
