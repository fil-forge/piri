package ucan

import (
	blobcaps "github.com/fil-forge/libforge/capabilities/blob"
	"github.com/fil-forge/libforge/digestutil"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/principal"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/fil-forge/piri/pkg/service/retrieval/handlers/spacecontent"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

// BlobRetrievalService is the surface the /blob/retrieve handler depends on.
// Unlike SpaceContentRetrievalService, no allocation store is required:
// `/blob/retrieve` is a service-level capability and does not check space
// membership.
type BlobRetrievalService interface {
	ID() principal.Signer
	Blobs() blobstore.BlobGetter
}

// NewBlobRetrieveHandler returns the /blob/retrieve UCAN handler.
//
// This is the service-level retrieval entry point — e.g. an indexer pulling
// content claims from a Piri node. The blob is fetched by digest alone, with
// no space allocation lookup; authorization is enforced entirely through the
// UCAN delegation chain on the inbound invocation.
//
// For user-facing, space-scoped retrieval with byte-range support see
// NewContentRetrieveHandler (bound to libforge `content.Retrieve`).
func NewBlobRetrieveHandler(service BlobRetrievalService) Handler {
	return Handler{
		Capability: blobcaps.Retrieve,
		Handler: bindexec.NewHandler(func(
			req *bindexec.Request[*blobcaps.RetrieveArguments],
			res *bindexec.Response[*blobcaps.RetrieveOK],
		) (err error) {
			ctx, span := tracer.Start(req.Context(), "blob.retrieve")
			defer func() {
				if err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
				}
				span.End()
			}()

			args := req.Task().Arguments()
			digest := args.Blob.Digest

			span.SetAttributes(
				attribute.String("digest", digestutil.Format(digest)),
				attribute.Stringer("issuer", req.Invocation().Issuer()),
			)
			log := log.With(
				"iss", req.Invocation().Issuer(),
				"digest", digestutil.Format(digest),
			)
			log.Debug("blob retrieve")

			// No byte-range support on /blob/retrieve — service-level
			// retrieval always returns the full blob.
			ctr, retErr := spacecontent.Retrieve(ctx, service.Blobs(), digest, nil)
			if retErr != nil {
				if ctr != nil {
					res.SetMetadata(ctr)
				}
				return res.SetFailure(retErr)
			}

			res.SetMetadata(ctr)
			return res.SetSuccess(&blobcaps.RetrieveOK{})
		}),
	}
}
