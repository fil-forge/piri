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
	"github.com/fil-forge/ucantone/binding"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/server"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/ucanhandlers"
)

// BlobAcceptedErrorName is the stable receipt-failure name when unallocate
// is invoked for a blob that has been accepted — accepted blobs are released
// via /blob/remove, never unallocated.
const BlobAcceptedErrorName = "BlobAccepted"

// UnallocateDeps is the dependency set populated by fx for the Unallocate
// handler.
type UnallocateDeps struct {
	fx.In
	ID          app.IdentityConfig
	Allocations AllocationRemover
	Acceptances AcceptanceChecker
	Pieces      PieceRemover
}

// AcceptanceChecker is the slice of acceptancestore.AcceptanceStore the
// Unallocate handler depends on.
type AcceptanceChecker interface {
	Exists(ctx context.Context, digest multihash.Multihash) (bool, error)
}

var _ AcceptanceChecker = (acceptancestore.AcceptanceStore)(nil)

func NewBlobUnallocateHandler(deps UnallocateDeps) server.Route {
	return blob.Unallocate.Route(func(req *binding.Request[*blob.UnallocateArguments], rsp *binding.Response[*blob.UnallocateOK]) error {
		args := req.Task().Arguments()

		// The invocation subject must be this storage provider; the space
		// abandoning its upload travels in the arguments. Authorization that
		// the upload service may invoke /blob/unallocate is enforced by the
		// validator's proof chain (rooted at the provider).
		if err := ucanhandlers.RequireSubject(req, deps.ID.Issuer.DID()); err != nil {
			return rsp.SetFailure(err)
		}

		if err := Unallocate(req.Context(), deps, &UnallocateRequest{
			Space:  args.Space,
			Digest: args.Digest,
		}); err != nil {
			var named errors.Named
			if errors.As(err, &named) {
				return rsp.SetFailure(named)
			}
			return err
		}

		return rsp.SetSuccess(&blob.UnallocateOK{})
	})
}

type UnallocateRequest struct {
	Space  did.DID
	Digest multihash.Multihash
}

// Unallocate retires a parked blob: it deletes the space's allocation for
// the digest and, when no space holds an allocation afterward, deletes the
// bytes. A blob with any acceptance is refused with BlobAccepted — accepted
// blobs carry claims and are released via /blob/remove. Idempotent:
// unallocating an unknown or already-unallocated blob succeeds.
func Unallocate(ctx context.Context, deps UnallocateDeps, req *UnallocateRequest) (err error) {
	ctx, span := tracer.Start(ctx, "blob.unallocate")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	log := log.With("blob", digestutil.Format(req.Digest))
	log.Infof("%s space: %s", blob.Unallocate.Command, req.Space)
	span.SetAttributes(
		attribute.Stringer("space.did", req.Space),
		attribute.Stringer("blob.digest", req.Digest),
	)

	// Unallocate operates strictly on parked blobs. An acceptance in ANY
	// space means the bytes carry claims (and may be aggregated) — the
	// caller must release its claim via /blob/remove instead.
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

	// Bytes are deleted only when no space holds an allocation — another
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

	// Never accepted, so RemovePiece takes the unaggregated fast path and
	// deletes the bytes immediately (and stays safe in the commp-codec edge
	// case where an upload-time mapping exists).
	if err := deps.Pieces.RemovePiece(ctx, req.Digest); err != nil {
		log.Errorw("removing parked piece", "error", err)
		return fmt.Errorf("removing parked piece: %w", err)
	}
	return nil
}
