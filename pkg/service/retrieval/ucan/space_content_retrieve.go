package ucan

import (
	"errors"
	"fmt"

	logging "github.com/ipfs/go-log/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	contentcaps "github.com/fil-forge/libforge/capabilities/content"
	"github.com/fil-forge/libforge/digestutil"
	ucantone_errors "github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/execution/bindexec"

	"github.com/fil-forge/piri/pkg/service/retrieval/handlers/spacecontent"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

var log = logging.Logger("retrieval/ucan")

type SpaceContentRetrievalService interface {
	Allocations() allocationstore.AllocationStore
	Blobs() blobstore.BlobGetter
}

// NewContentRetrieveHandler returns the /content/retrieve UCAN handler. The
// space is the invocation's Subject. The handler verifies an allocation
// exists for (space, blob digest), then streams the blob bytes back to the
// caller via [retrieval.HTTPHeaderResponseContainer] in response metadata.
func NewContentRetrieveHandler(service SpaceContentRetrievalService) Handler {
	return Handler{
		Capability: contentcaps.Retrieve,
		Handler: bindexec.NewHandler(func(
			req *bindexec.Request[*contentcaps.RetrieveArguments],
			res *bindexec.Response[*contentcaps.RetrieveOK],
		) (err error) {
			ctx, span := tracer.Start(req.Context(), "content.retrieve")
			defer func() {
				if err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
				}
				span.End()
			}()

			args := req.Task().Arguments()
			space := req.Invocation().Subject()
			digest := args.Blob.Digest
			digestStr := digestutil.Format(digest)
			start := args.Range.Start
			end := args.Range.End

			span.SetAttributes(
				attribute.Stringer("space.did", space),
				attribute.String("digest", digestStr),
				attribute.Int64("range.start", int64(start)),
				attribute.Int64("range.end", int64(end)),
				attribute.Stringer("issuer", req.Invocation().Issuer()),
			)

			log := log.With(
				"iss", req.Invocation().Issuer(),
				"with", space.String(),
				"digest", digestStr,
				"range", fmt.Sprintf("%d-%d", start, end),
			)

			// Check that we have an allocation for this (space, blob).
			if _, err := service.Allocations().Get(ctx, digest, space); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					log.Debug("allocation not found")
					return res.SetFailure(ucantone_errors.New(
						spacecontent.NotFoundErrorName,
						"allocation not found: %s",
						digestStr,
					))
				}
				log.Errorw("getting allocation", "error", err)
				return fmt.Errorf("getting allocation: %w", err)
			}

			byteRange := &blobstore.Range{Start: start, End: &end}
			ctr, retErr := spacecontent.Retrieve(ctx, service.Blobs(), digest, byteRange)
			if retErr != nil {
				// Container carries the HTTP status that matches the failure.
				if ctr != nil {
					res.SetMetadata(ctr)
				}
				return res.SetFailure(retErr)
			}

			res.SetMetadata(ctr)
			return res.SetSuccess(&contentcaps.RetrieveOK{})
		}),
	}
}
