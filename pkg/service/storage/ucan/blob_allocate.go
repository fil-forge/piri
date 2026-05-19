package ucan

import (
	"github.com/fil-forge/libforge/capabilities/blob"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/execution/bindexec"

	blobhandler "github.com/fil-forge/piri/pkg/service/storage/handlers/blob"
)

const maxUploadSize = 127 * (1 << 25)

// BlobSizeLimitExceededErrorName is the stable receipt-failure name when
// the requested allocation exceeds maxUploadSize.
const BlobSizeLimitExceededErrorName = "BlobSizeLimitExceeded"

func NewBlobAllocateHandler(deps blobhandler.AllocateDeps) Handler {
	return TypedHandler(
		blob.Allocate,
		func(req *bindexec.Request[*blob.AllocateArguments], rsp *bindexec.Response[*blob.AllocateOK]) error {
			args := req.Task().Arguments()

			if err := requireSubject(req, deps.ID.Signer.DID()); err != nil {
				return rsp.SetFailure(err)
			}

			// TODO(forrest)[ucan1]: reconcile with blob.MaxBlobSize
			// (256 MiB). piri's maxUploadSize is intentionally larger
			// (~4.06 GiB) to support legacy upload sizes.
			if args.Blob.Size > maxUploadSize {
				return rsp.SetFailure(errors.New(
					BlobSizeLimitExceededErrorName,
					"blob size %d exceeds maximum %d", args.Blob.Size, maxUploadSize,
				))
			}

			resp, err := blobhandler.Allocate(req.Context(), deps, &blobhandler.AllocateRequest{
				Space: req.Task().Subject(),
				Blob:  args.Blob,
				Cause: args.Cause,
			})
			if err != nil {
				return err
			}

			ok := &blob.AllocateOK{Size: resp.Size}
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
