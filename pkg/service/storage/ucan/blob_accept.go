package ucan

import (
	blobcaps "github.com/fil-forge/libforge/capabilities/blob"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/ucan/container"

	"github.com/fil-forge/piri/pkg/pdp"
	"github.com/fil-forge/piri/pkg/service/blobs"
	"github.com/fil-forge/piri/pkg/service/claims"
	blobhandler "github.com/fil-forge/piri/pkg/service/storage/handlers/blob"
)

type BlobAcceptService interface {
	ID() principal.Signer
	PDP() pdp.PDP
	Blobs() blobs.Blobs
	Claims() claims.Claims
}

// NewBlobAcceptHandler returns the /blob/accept UCAN handler. The space is
// the invocation's Subject. Output invocations (the location commitment claim,
// and optionally a /pdp/accept) are returned to the caller via the response
// container metadata; the typed AcceptOK record only carries `Site` (the CID
// of the claim invocation).
func NewBlobAcceptHandler(storageService BlobAcceptService) Handler {
	return Handler{
		Capability: blobcaps.Accept,
		Handler: bindexec.NewHandler(func(
			req *bindexec.Request[*blobcaps.AcceptArguments],
			res *bindexec.Response[*blobcaps.AcceptOK],
		) error {
			args := req.Task().Arguments()

			resp, err := blobhandler.Accept(req.Context(), storageService, &blobhandler.AcceptRequest{
				Space: req.Invocation().Subject(),
				Blob:  args.Blob,
				Put:   args.Put,
				Cause: req.Invocation().Task().Link(),
			})
			if err != nil {
				return res.SetFailure(err)
			}

			metaOpts := []container.Option{container.WithInvocations(resp.Claim)}
			if resp.PDP != nil {
				metaOpts = append(metaOpts, container.WithInvocations(resp.PDP))
			}
			if err := res.SetMetadata(container.New(metaOpts...)); err != nil {
				return res.SetFailure(err)
			}

			return res.SetSuccess(&blobcaps.AcceptOK{
				Site: resp.Claim.Link(),
			})
		}),
	}
}
