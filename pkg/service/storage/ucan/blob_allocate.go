package ucan

import (
	blobcaps "github.com/fil-forge/libforge/capabilities/blob"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/principal"

	"github.com/fil-forge/piri/pkg/pdp"
	"github.com/fil-forge/piri/pkg/service/blobs"
	blobhandler "github.com/fil-forge/piri/pkg/service/storage/handlers/blob"
)

const maxUploadSize = 127 * (1 << 25)

type BlobAllocateService interface {
	ID() principal.Signer
	PDP() pdp.PDP
	Blobs() blobs.Blobs
}

// NewBlobAllocateHandler returns the /blob/allocate UCAN handler. The space is
// read from the invocation's Subject (the entity being invoked-against, i.e.
// the space the allocation is for). Authorization (only delegated callers can
// invoke against a space) is enforced by the validator/dispatcher.
func NewBlobAllocateHandler(storageService BlobAllocateService) Handler {
	return Handler{
		Capability: blobcaps.Allocate,
		Handler: bindexec.NewHandler(func(
			req *bindexec.Request[*blobcaps.AllocateArguments],
			res *bindexec.Response[*blobcaps.AllocateOK],
		) error {
			args := req.Task().Arguments()

			if args.Blob.Size > maxUploadSize {
				return res.SetFailure(NewBlobSizeLimitExceededError(args.Blob.Size, maxUploadSize))
			}

			resp, err := blobhandler.Allocate(req.Context(), storageService, &blobhandler.AllocateRequest{
				Space: req.Invocation().Subject(),
				Blob:  args.Blob,
				Cause: req.Invocation().Task().Link(),
			})
			if err != nil {
				return res.SetFailure(err)
			}

			return res.SetSuccess(&blobcaps.AllocateOK{
				Size:    resp.Size,
				Address: resp.Address,
			})
		}),
	}
}
