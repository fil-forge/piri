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

	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
	"github.com/fil-forge/piri/pkg/ucanhandlers"
)

// ReleaseDeps is the dependency set populated by fx for the Release handler.
type ReleaseDeps struct {
	fx.In
	ID          identity.Identity
	Allocations AllocationRemover
	Acceptances AcceptanceRemover
	ClaimStore  invocationstore.InvocationStore
	Pieces      PieceRemover
}

// AllocationRemover is the slice of allocationstore.AllocationStore the
// Remove handler depends on.
type AllocationRemover interface {
	Delete(ctx context.Context, digest multihash.Multihash, space did.DID) error
	ListSpaces(ctx context.Context, digest multihash.Multihash) ([]did.DID, error)
}

// AcceptanceRemover is the slice of acceptancestore.AcceptanceStore the
// Remove handler depends on.
type AcceptanceRemover interface {
	Get(ctx context.Context, digest multihash.Multihash, space did.DID) (acceptance.Acceptance, error)
	Delete(ctx context.Context, digest multihash.Multihash, space did.DID) error
	ListSpaces(ctx context.Context, digest multihash.Multihash) ([]did.DID, error)
}

// PieceRemover is the slice of the PDP piece-remover API the Release handler
// depends on.
type PieceRemover interface {
	RemovePiece(ctx context.Context, blob multihash.Multihash) error
}

var (
	_ AllocationRemover = (allocationstore.AllocationStore)(nil)
	_ AcceptanceRemover = (acceptancestore.AcceptanceStore)(nil)
	_ PieceRemover      = (pdptypes.PieceRemoverAPI)(nil)
)

func NewBlobReleaseHandler(deps ReleaseDeps) server.Route {
	return blob.Release.Route(func(req *binding.Request[*blob.ReleaseArguments], rsp *binding.Response[*blob.ReleaseOK]) error {
		args := req.Task().Arguments()

		// The invocation subject must be this storage provider; the space
		// releasing its claim travels in the arguments. Authorization that
		// the upload service may invoke /blob/release is enforced by the
		// validator's proof chain (rooted at the provider).
		if err := ucanhandlers.RequireSubject(req, deps.ID.DID()); err != nil {
			return rsp.SetFailure(err)
		}

		if err := Release(req.Context(), deps, &ReleaseRequest{
			Space:  args.Space,
			Digest: args.Digest,
		}); err != nil {
			return err
		}

		return rsp.SetSuccess(&blob.ReleaseOK{})
	})
}

type ReleaseRequest struct {
	Space  did.DID
	Digest multihash.Multihash
}

// Release releases a space's claim on an accepted blob: it deletes the
// space's allocation, acceptance, and location claim for the digest, and —
// when no space holds a claim on the digest afterward — queues the bytes
// for release via the PDP piece remover. Physical deletion is always
// asynchronous: the removal machinery re-verifies zero claims and retires
// proven pieces on-chain before deleting. Idempotent: removing an unknown
// blob or an already-released claim succeeds.
func Release(ctx context.Context, deps ReleaseDeps, req *ReleaseRequest) (err error) {
	ctx, span := tracer.Start(ctx, "blob.release")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	log := log.With("blob", digestutil.Format(req.Digest))
	log.Infof("%s space: %s", blob.Release.Command, req.Space)
	span.SetAttributes(
		attribute.Stringer("space.did", req.Space),
		attribute.Stringer("blob.digest", req.Digest),
	)

	// Delete the space's location claim first (found via the acceptance
	// record) so a failure never leaves a claim without its acceptance.
	acc, err := deps.Acceptances.Get(ctx, req.Digest, req.Space)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		log.Errorw("getting acceptance", "error", err)
		return fmt.Errorf("getting acceptance: %w", err)
	}
	if err == nil && acc.Site.Defined() {
		if err := deps.ClaimStore.Delete(ctx, acc.Site); err != nil {
			log.Errorw("deleting location claim", "error", err)
			return fmt.Errorf("deleting location claim: %w", err)
		}
	}

	if err := deps.Acceptances.Delete(ctx, req.Digest, req.Space); err != nil {
		log.Errorw("deleting acceptance", "error", err)
		return fmt.Errorf("deleting acceptance: %w", err)
	}
	if err := deps.Allocations.Delete(ctx, req.Digest, req.Space); err != nil {
		log.Errorw("deleting allocation", "error", err)
		return fmt.Errorf("deleting allocation: %w", err)
	}

	// Physical deletion is gated on zero claims across all spaces, in both
	// stores: a live allocation means an upload may still be in flight.
	acceptSpaces, err := deps.Acceptances.ListSpaces(ctx, req.Digest)
	if err != nil {
		log.Errorw("listing acceptance spaces", "error", err)
		return fmt.Errorf("listing acceptance spaces: %w", err)
	}
	allocSpaces, err := deps.Allocations.ListSpaces(ctx, req.Digest)
	if err != nil {
		log.Errorw("listing allocation spaces", "error", err)
		return fmt.Errorf("listing allocation spaces: %w", err)
	}
	if len(acceptSpaces) > 0 || len(allocSpaces) > 0 {
		log.Infow("blob still claimed, retaining bytes",
			"acceptances", len(acceptSpaces), "allocations", len(allocSpaces))
		return nil
	}

	if err := deps.Pieces.RemovePiece(ctx, req.Digest); err != nil {
		log.Errorw("removing piece", "error", err)
		return fmt.Errorf("removing piece: %w", err)
	}
	return nil
}
