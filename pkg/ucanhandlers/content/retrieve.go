package content

import (
	stderrors "errors"

	"github.com/fil-forge/libforge/commands/content"
	"github.com/fil-forge/ucantone/binding"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/server"
	fxlib "go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/blobstore"
	"github.com/fil-forge/piri/pkg/ucanhandlers/blob"
)

// NotAllocatedErrorName is the receipt-failure name for a content/retrieve
// request whose (space, digest) pair has no allocation on this node.
const NotAllocatedErrorName = "NotAllocated"

// RetrieveDeps is the dependency set populated by fx for the
// space/content/retrieve UCAN method.
type RetrieveDeps struct {
	fxlib.In

	ID          principal.Signer
	Allocations allocationstore.AllocationStore
	Pieces      types.PieceReaderAPI
}

func NewRetrieveHandler(deps RetrieveDeps) server.Route {
	return server.NewRoute(
		content.Retrieve,
		func(req *binding.Request[*content.RetrieveArguments], rsp *binding.Response[*content.RetrieveOK]) error {
			args := req.Task().Arguments()
			ctx := req.Context()

			// space/content/retrieve is space-scoped: the invocation
			// subject is the space, and the blob must have an allocation
			// in that space before we'll stream it.
			space := req.Task().Subject()
			if _, err := deps.Allocations.Get(ctx, args.Blob.Digest, space); err != nil {
				if stderrors.Is(err, store.ErrNotFound) {
					return rsp.SetFailure(errors.New(
						NotAllocatedErrorName,
						"no allocation for blob %s in space %s",
						args.Blob.Digest.B58String(), space,
					))
				}
				return err
			}

			byteRange := contentRangeToBlobstoreRange(args.Range)
			container, derr := blob.Retrieve(ctx, deps.Pieces, args.Blob.Digest, byteRange)
			if err := rsp.SetMetadata(container); err != nil {
				return err
			}
			if derr != nil {
				return rsp.SetFailure(derr)
			}
			return rsp.SetSuccess(&content.RetrieveOK{})
		},
	)
}

// contentRangeToBlobstoreRange converts the value-typed Range from the
// content schema into the pointer-End shape blobstore expects. End is
// inclusive in both representations.
func contentRangeToBlobstoreRange(r content.Range) *blobstore.Range {
	end := r.End
	return &blobstore.Range{Start: r.Start, End: &end}
}
