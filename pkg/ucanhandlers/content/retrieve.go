package content

import (
	stderrors "errors"
	"fmt"
	"net/http"
	"os"

	"github.com/fil-forge/libforge/commands/content"
	"github.com/fil-forge/ucantone/binding"
	"github.com/fil-forge/ucantone/server"
	logging "github.com/ipfs/go-log/v2"
	fxlib "go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/blobstore"
	"github.com/fil-forge/piri/pkg/ucanhandlers/blob"
)

var log = logging.Logger("ucanhandlers/content")

// NotAllocatedErrorName is the receipt-failure name for a content/retrieve
// request whose (space, digest) pair has no allocation on this node.
const NotAllocatedErrorName = "NotAllocated"

// RetrieveDeps is the dependency set populated by fx for the
// space/content/retrieve UCAN method.
type RetrieveDeps struct {
	fxlib.In

	Allocations allocationstore.AllocationStore
	Pieces      types.PieceReaderAPI
}

func NewRetrieveHandler(deps RetrieveDeps) server.Route {
	return content.Retrieve.Route(func(req *binding.Request[*content.RetrieveArguments], rsp *binding.Response[*content.RetrieveOK]) error {
		args := req.Task().Arguments()
		ctx := req.Context()

		// space/content/retrieve is space-scoped: the invocation
		// subject is the space, and the blob must have an allocation
		// in that space before we'll stream it.
		//
		// Both failure paths below MUST set HTTP metadata in addition to
		// the receipt failure: callers like the indexing service inspect
		// the HTTP status to decide whether to read the body, so a bare
		// SetFailure leaves the response a misleading 200 OK with an
		// empty body and the failure buried inside the receipt envelope.
		space := req.Task().Subject()
		// stderr bypass — confirms code path is reached even if logger is filtered
		fmt.Fprintf(os.Stderr, "STDERR_DIAG content/retrieve invoked space=%s digest=%s\n",
			space.String(), args.Blob.Digest.B58String())
		log.Warnw("content/retrieve invoked",
			"space", space.String(),
			"digest", args.Blob.Digest.B58String(),
			"range_start", args.Range.Start,
			"range_end", args.Range.End,
		)
		if _, err := deps.Allocations.Get(ctx, args.Blob.Digest, space); err != nil {
			var status int
			var name, message string
			if stderrors.Is(err, store.ErrNotFound) {
				status = http.StatusNotFound
				name = NotAllocatedErrorName
				message = fmt.Sprintf("no allocation for blob %s in space %s", args.Blob.Digest.B58String(), space)
			} else {
				status = http.StatusInternalServerError
				name = blob.InternalServerErrorName
				message = fmt.Sprintf("looking up allocation for blob %s in space %s: %s", args.Blob.Digest.B58String(), space, err)
			}
			container, derr := blob.ErrorResponse(status, name, message)
			if merr := rsp.SetMetadata(container); merr != nil {
				return merr
			}
			return rsp.SetFailure(derr)
		}

		byteRange := contentRangeToBlobstoreRange(args.Range)
		container, derr := blob.Retrieve(ctx, deps.Pieces, args.Blob.Digest, byteRange)
		log.Warnw("content/retrieve result",
			"status", container.StatusCode,
			"content_length", container.Header.Get("Content-Length"),
			"derr", derr,
		)
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
