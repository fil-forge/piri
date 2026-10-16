package blob

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/fil-forge/libforge/commands"
	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/ucantone/binding"
	"github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/fx"

	"github.com/fil-forge/libforge/commands/assert"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/commands/pdp"
	"github.com/fil-forge/libforge/digestutil"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"

	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/service/publisher"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
)

// InternalErrorName is the stable receipt-failure name for invariant
// violations the handler hits at runtime (e.g., a link type we expected
// to be a cidlink wasn't).
const InternalErrorName = "InternalError"

// AcceptDeps is the dependency set populated by fx for the Accept handler.
type AcceptDeps struct {
	fx.In
	ID          identity.Identity
	Acceptances AcceptanceStore
	Pieces      PieceReader
	Commp       commp.Calculator
	ClaimStore  invocationstore.InvocationStore
	Publisher   publisher.Publisher
	// Pending resolves the digest of an accept that names only a digest
	// code. Only the handler uses it; Accept itself always has a digest.
	Pending PendingAllocations
	// Tx runs the accept's writes as one transaction.
	Tx store.Transactor
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

func NewAcceptHandler(deps AcceptDeps) server.Route {
	return blob.Accept.Route(func(req *binding.Request[*blob.AcceptArguments], rsp *binding.Response[*blob.AcceptOK]) error {
		args := req.Task().Arguments()

		// The route's middleware has already required an invocation subjected to
		// this provider and issued by someone it delegated to (pkg/ucanhandlers).

		digest, hashed := args.Blob.Digest()
		b := blob.Blob{Digest: digest, Size: args.Blob.Size()}
		var pending allocation.Pending
		if !hashed {
			digest, p, err := resolvePutDigest(req.Context(), deps.Pending, req.Metadata(), args.Space, args.Blob, args.Put)
			if err != nil {
				var named errors.Named
				if errors.As(err, &named) {
					return rsp.SetFailure(named)
				}
				return err
			}
			b.Digest = digest
			pending = p
		}

		accReq := &AcceptRequest{
			Space: args.Space,
			Blob:  b,
			Put:   args.Put,
			Cause: req.Invocation().Task().Link(),
		}
		if !hashed {
			accReq.Allocation = &pending.Allocation
			accReq.WithoutDigest = true
		} else if link, ok := putAllocation(req.Metadata(), args.Put.Task, digest); ok {
			accReq.Allocation = &link
		}
		resp, err := Accept(req.Context(), deps, accReq)
		if err != nil {
			return err
		}

		if err := rsp.SetMetadata(container.New(container.WithInvocations(resp.Claim, resp.PDP))); err != nil {
			return fmt.Errorf("setting metadata on response: %w", err)
		}
		return rsp.SetSuccess(&blob.AcceptOK{
			Site: resp.Claim.Link(),
			PDP:  promise.AwaitOK{Task: resp.PDP.Task().Link()},
		})
	},
	)
}

type AcceptRequest struct {
	Space did.DID
	Blob  blob.Blob
	Put   promise.AwaitOK
	// Cause is a link to the `blob/accept` or `blob/replica/transfer` invocation.
	Cause cid.Cid
	// Allocation links the `/blob/allocate` task whose allocation is accepted,
	// when the accept identifies it. It is recorded on the acceptance, which
	// a reject of that allocation refuses on.
	Allocation *cid.Cid
	// WithoutDigest says Allocation was made without a digest, so its pending
	// record is what says it has not been released.
	WithoutDigest bool
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

	log := log.With("blob", digestutil.Format(req.Blob.Digest))
	log.Infof("%s %s", blob.Accept.Command, req.Space)
	span.SetAttributes(
		attribute.Stringer("space.did", req.Space),
		attribute.Stringer("blob.digest", req.Blob.Digest),
		attribute.Int64("blob.size", int64(req.Blob.Size)),
	)

	// get a download url
	blobCID := cid.NewCidV1(cid.Raw, req.Blob.Digest)
	loc, err := deps.Pieces.ReadPieceURL(blobCID)
	if err != nil {
		log.Errorw("creating retrieval URL for blob", "error", err)
		return nil, fmt.Errorf("creating retrieval URL for blob: %w", err)
	}
	// generate the invocation that will complete when aggregation is complete and the piece is accepted
	// The task has no nonce so its link is deterministic: the aggregation
	// pipeline issues the receipt against a rebuilt invocation and /pdp/info
	// looks it up the same way — all three must derive the same task link.
	pdpAcceptInv, err := pdp.Accept.Invoke(
		deps.ID,
		deps.ID.DID(),
		&pdp.AcceptArguments{
			Blob: req.Blob.Digest,
		},
		invocation.WithNoExpiration(),
		invocation.WithNoNonce(),
	)
	if err != nil {
		log.Error("creating piece accept invocation", "error", err)
		return nil, fmt.Errorf("creating piece accept invocation: %w", err)
	}

	// Range.End is inclusive, so the last byte offset is size-1.
	// TODO(forrest)[ucan1]: this feels wrong — accept should have its own
	// blob-size type whose range End cannot be nil, rather than threading a
	// pointer-to-uint64 here. See #12.
	rangeEnd := req.Blob.Size - 1
	claim, err := assert.Location.Invoke(
		deps.ID,
		deps.ID.DID(),
		&assert.LocationArguments{
			Space:    req.Space,
			Content:  req.Blob.Digest,
			Location: []commands.CborURL{commands.CborURL(loc)},
			Range:    &assert.Range{Start: 0, End: &rangeEnd},
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
		PDPAccept:  promise.AwaitOK{Task: pdpAcceptInv.Task().Link()},
		// The claim link is the digest→claim index /blob/release uses to
		// delete the location claim when this space's acceptance is removed.
		Site:       claim.Link(),
		Allocation: req.Allocation,
	}
	// The accept's records commit together or not at all: the acceptance,
	// the blob's entry in the PDP pipeline, the allocation it accepted, the
	// location claim and its queued advertisement. /blob/reject's
	// BlobAccepted guard and the removal machinery's claim re-checks read
	// the acceptance as a sign the blob entered the pipeline, which the one
	// commit makes exact. The pipeline's task is spawned, and the claim
	// cached with the indexing service, once the records have committed.
	// The allocation and the blob are locked first, as a reject, a release,
	// the expiry task or the removal sweep locks them, so they run one after
	// the other: a reject that comes later sees the acceptance, an allocation
	// released first is not accepted, and bytes the sweep removed first are
	// not found.
	err = deps.Tx.Run(ctx, func(ctx context.Context) error {
		if err := lockAcceptedAllocation(ctx, deps, req); err != nil {
			return err
		}
		if err := deps.Acceptances.Put(ctx, acc); err != nil {
			log.Errorw("putting acceptance for blob", "error", err)
			return fmt.Errorf("putting acceptance for blob: %w", err)
		}

		// submit the piece for aggregation
		if err := deps.Commp.Enqueue(ctx, req.Blob.Digest); err != nil {
			log.Errorw("submitting piece for aggregation", "error", err)
			return fmt.Errorf("submitting piece for aggregation: %w", err)
		}

		// The allocation the acceptance accepted becomes the space's
		// allocation of the blob again if a later allocation of the same
		// content replaced it: a reject of it then finds the acceptance, and
		// a reject of the other finds nothing to drop.
		if req.Allocation != nil {
			if err := deps.Pending.MakeCurrent(ctx, req.Blob.Digest, req.Space, *req.Allocation); err != nil {
				log.Errorw("recording accepted allocation", "error", err)
				return fmt.Errorf("recording accepted allocation: %w", err)
			}
		}

		if err := deps.ClaimStore.Put(ctx, claim); err != nil {
			log.Errorw("putting location claim for blob", "error", err)
			return fmt.Errorf("putting location claim for blob: %w", err)
		}

		if err := deps.Publisher.Publish(ctx, claim); err != nil {
			log.Errorw("publishing location commitment", "error", err)
			return fmt.Errorf("publishing location commitment: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &AcceptResponse{
		Claim: claim,
		PDP:   pdpAcceptInv,
	}, nil
}

// lockAcceptedAllocation locks the allocation the accept accepts and the
// blob, in the order allocationstore.AllocationStore.Lock describes, and
// refuses an allocation released, or bytes removed, before the locks were
// taken. An allocation made without a digest is released once its pending
// record is gone; one made with a digest, once the space holds no allocation
// of the blob. An accept that does not identify its allocation accepts
// whatever the space holds. The bytes are checked under the blob's lock,
// which the removal sweep takes before it deletes them.
func lockAcceptedAllocation(ctx context.Context, deps AcceptDeps, req *AcceptRequest) error {
	allocs := deps.Pending
	if req.Allocation != nil && req.WithoutDigest {
		if err := allocs.LockPending(ctx, *req.Allocation); err != nil {
			log.Errorw("locking pending allocation", "error", err)
			return fmt.Errorf("locking pending allocation: %w", err)
		}
		if _, err := allocs.GetPending(ctx, *req.Allocation); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("allocation %s was released before it was accepted", *req.Allocation)
			}
			return fmt.Errorf("getting pending allocation: %w", err)
		}
	}
	if err := allocs.LockBlob(ctx, req.Blob.Digest); err != nil {
		log.Errorw("locking blob", "error", err)
		return fmt.Errorf("locking blob: %w", err)
	}
	// ensure the blob exists, else it cannot be accepted.
	found, err := deps.Pieces.Has(ctx, req.Blob.Digest)
	if err != nil {
		log.Errorw("finding piece for blob", "error", err)
		return fmt.Errorf("finding piece for blob: %w", err)
	}
	if !found {
		log.Errorw("piece not found", "blob", req.Blob.Digest)
		return fmt.Errorf("piece not found")
	}
	if err := allocs.Lock(ctx, req.Blob.Digest, req.Space); err != nil {
		log.Errorw("locking allocation", "error", err)
		return fmt.Errorf("locking allocation: %w", err)
	}
	if req.Allocation != nil && !req.WithoutDigest {
		if _, err := allocs.Get(ctx, req.Blob.Digest, req.Space); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("allocation %s was released before it was accepted", *req.Allocation)
			}
			return fmt.Errorf("getting allocation: %w", err)
		}
	}
	return nil
}
