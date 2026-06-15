package blob

import (
	"context"
	"fmt"
	"net/url"

	"github.com/fil-forge/libforge/commands"
	"github.com/fil-forge/ucantone/binding"
	"github.com/fil-forge/ucantone/server"
	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multihash"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/fx"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/digestutil"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/ucan"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/presets"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
	"github.com/fil-forge/piri/pkg/ucanhandlers"
)

var log = logging.Logger("storage/handlers/blob")

const maxUploadSize = blob.MaxBlobSize

// BlobSizeLimitExceededErrorName is the stable receipt-failure name when
// the requested allocation exceeds maxUploadSize.
const BlobSizeLimitExceededErrorName = "BlobSizeLimitExceeded"

// AllocateDeps is the dependency set populated by fx for the Allocate
// handler.
type AllocateDeps struct {
	fx.In
	ID          app.IdentityConfig
	Allocations AllocationStore
	Pieces      PieceAllocator
}

// AllocationStore is the slice of allocationstore.AllocationStore the
// allocation handler depends on.
type AllocationStore interface {
	Get(ctx context.Context, digest multihash.Multihash, space did.DID) (allocation.Allocation, error)
	Exists(ctx context.Context, digest multihash.Multihash) (bool, error)
	Put(ctx context.Context, alloc allocation.Allocation) error
}

// PieceAllocator is the slice of the PDP piece API the allocation handler
// depends on.
type PieceAllocator interface {
	Has(ctx context.Context, digest multihash.Multihash) (bool, error)
	AllocatePiece(ctx context.Context, alloc types.PieceAllocation) (*types.AllocatedPiece, error)
	WritePieceURL(uploadID uuid.UUID) (url.URL, error)
}

// Compile-time check that the concrete production types satisfy the narrow
// interfaces this handler declares.
var (
	_ AllocationStore = (allocationstore.AllocationStore)(nil)
	_ PieceAllocator  = (types.PieceAPI)(nil)
)

func NewBlobAllocateHandler(deps AllocateDeps) server.Route {
	return blob.Allocate.Route(func(req *binding.Request[*blob.AllocateArguments], rsp *binding.Response[*blob.AllocateOK]) error {
		args := req.Task().Arguments()

		// The invocation subject must be this storage provider; the space being
		// allocated into travels in the arguments. Authorization that the upload
		// service may invoke /blob/allocate is enforced by the validator's proof
		// chain (rooted at the provider).
		if err := ucanhandlers.RequireSubject(req, deps.ID.Signer.DID()); err != nil {
			return rsp.SetFailure(err)
		}

		// TODO(forrest)[ucan1]: reconcile with blob.MaxBlobSize
		// to ensure it matches the constraints of piri, namely the aggregation pipeline to adding roots
		if args.Blob.Size > maxUploadSize {
			return rsp.SetFailure(errors.New(
				BlobSizeLimitExceededErrorName,
				"blob size %d exceeds maximum %d", args.Blob.Size, maxUploadSize,
			))
		}

		resp, err := Allocate(req.Context(), deps, &AllocateRequest{
			Space: args.Space,
			Blob:  args.Blob,
			Cause: args.Cause,
		})
		if err != nil {
			return err
		}

		ok := &blob.AllocateOK{Size: resp.Size}
		// returning an address is optional, when not returned it means the data is already allocated
		// and not upload is required. Rather just call Accept.
		if resp.Address != nil {
			ok.Address = &blob.BlobAddress{
				URL:     resp.Address.URL,
				Headers: resp.Address.Headers,
				Expires: resp.Address.Expires,
			}
		}
		return rsp.SetSuccess(ok)
	},
	)
}

type AllocateRequest struct {
	Space did.DID
	Blob  blob.Blob
	Cause cid.Cid
}

type AllocateResponse struct {
	Size    uint64
	Address *blob.BlobAddress
}

func Allocate(ctx context.Context, deps AllocateDeps, req *AllocateRequest) (resp *AllocateResponse, err error) {
	ctx, span := tracer.Start(ctx, "blob.allocate")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	log := log.With("blob", digestutil.Format(req.Blob.Digest))
	log.Infof("%s space: %s", blob.Allocate.Command, req.Space)
	span.SetAttributes(
		attribute.Stringer("space.did", req.Space),
		attribute.Stringer("blob.digest", req.Blob.Digest),
		attribute.Int64("blob.size", int64(req.Blob.Size)),
	)

	// check if we already have an allocation for the blob in this space
	_, err = deps.Allocations.Get(ctx, req.Blob.Digest, req.Space)
	allocated := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		log.Errorw("getting allocation", "error", err)
		return nil, fmt.Errorf("getting allocation: %w", err)
	}

	// check if any allocation exists for the blob (skip if we already found one above)
	anyAllocation := allocated
	if !allocated {
		anyAllocation, err = deps.Allocations.Exists(ctx, req.Blob.Digest)
		if err != nil {
			log.Errorw("checking allocation exists", "error", err)
			return nil, fmt.Errorf("checking allocation exists: %w", err)
		}
	}

	received := false
	// check if we received the blob (only possible if we have an allocation)
	if anyAllocation {
		has, err := deps.Pieces.Has(ctx, req.Blob.Digest)
		if err != nil {
			return nil, fmt.Errorf("getting blob: %w", err)
		}
		received = has
	}

	// the size reported in the receipt is the number of bytes allocated
	// in the space - if a previous allocation already exists, this has
	// already been done, so the allocation size is 0
	size := req.Blob.Size
	if allocated {
		log.Info("blob allocation already exists")
		size = 0
	}

	// nothing to do
	if allocated && received {
		log.Info("blob already received")
		return &AllocateResponse{
			Size: size,
			// NB: blob already received, therefor no address is needed for upload.
			Address: nil,
		}, nil
	}

	expiresIn := ucan.UnixTimestamp(60 * 60 * 24) // 1 day
	expiresAt := ucan.Now() + expiresIn

	var address *blob.BlobAddress
	// if not received yet, we need to generate an upload URL via PDP and
	// include it in the receipt.
	if !received {
		dmh, err := multihash.Decode(req.Blob.Digest)
		if err != nil {
			log.Errorw("decoding digest", "error", err)
			return nil, fmt.Errorf("decoding digest: %w", err)
		}
		if _, ok := presets.HasherRegistry[dmh.Name]; !ok {
			return nil, fmt.Errorf("unsupported hash: %s", dmh.Name)
		}
		// TODO we need to provide backpressure to the upload service here
		// based on the number of roots we are currently allocating.
		alloc, err := deps.Pieces.AllocatePiece(ctx, types.PieceAllocation{
			Piece: types.Piece{
				Name: dmh.Name,
				Hash: req.Blob.Digest,
				Size: int64(req.Blob.Size),
			},
		})
		if err != nil {
			log.Errorw("adding to pdp service", "error", err)
			return nil, fmt.Errorf("adding to pdp service: %w", err)
		}
		var uploadURL url.URL
		if alloc.Allocated {
			uploadURL, err = deps.Pieces.WritePieceURL(alloc.UploadID)
			if err != nil {
				log.Errorw("getting piece write URL", "error", err)
				return nil, fmt.Errorf("getting piece write URL: %w", err)
			}
		}
		address = &blob.BlobAddress{
			URL:     commands.CborURL(uploadURL),
			Expires: int64(expiresAt),
		}
	}

	// even if a previous allocation was made in this space, we create
	// another for the new invocation.
	err = deps.Allocations.Put(ctx, allocation.Allocation{
		Space:   req.Space,
		Blob:    req.Blob,
		Expires: expiresAt,
		Cause:   req.Cause,
	})
	if err != nil {
		log.Errorw("putting allocation", "error", err)
		return nil, fmt.Errorf("putting allocation: %w", err)
	}

	return &AllocateResponse{
		Size:    size,
		Address: address,
	}, nil
}
