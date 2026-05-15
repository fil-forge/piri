package ucan

import (
	"context"

	"github.com/fil-forge/go-libstoracha/capabilities/blob"
	"github.com/fil-forge/go-ucanto/core/invocation"
	"github.com/fil-forge/go-ucanto/core/receipt/fx"
	"github.com/fil-forge/go-ucanto/core/result"
	"github.com/fil-forge/go-ucanto/core/result/failure"
	"github.com/fil-forge/go-ucanto/server"
	"github.com/fil-forge/go-ucanto/ucan"

	blobhandler "github.com/fil-forge/piri/pkg/service/storage/handlers/blob"
)

const maxUploadSize = 127 * (1 << 25)

func WithBlobAllocateMethod(deps blobhandler.AllocateDeps) server.Option {
	return server.WithServiceMethod(
		blob.AllocateAbility,
		server.Provide(
			blob.Allocate,
			func(ctx context.Context, cap ucan.Capability[blob.AllocateCaveats], inv invocation.Invocation, iCtx server.InvocationContext) (result.Result[blob.AllocateOk, failure.IPLDBuilderFailure], fx.Effects, error) {
				//
				// UCAN Validation
				//

				// only service principal can perform an allocation
				if cap.With() != iCtx.ID().DID().String() {
					return result.Error[blob.AllocateOk, failure.IPLDBuilderFailure](NewUnsupportedCapabilityError(cap)), nil, nil
				}

				// enforce max upload size requirements
				if cap.Nb().Blob.Size > maxUploadSize {
					return result.Error[blob.AllocateOk, failure.IPLDBuilderFailure](NewBlobSizeLimitExceededError(cap.Nb().Blob.Size, maxUploadSize)), nil, nil
				}

				//
				// end UCAN Validation
				//

				resp, err := blobhandler.Allocate(ctx, deps, &blobhandler.AllocateRequest{
					Space: cap.Nb().Space,
					Blob:  cap.Nb().Blob,
					Cause: inv.Link(),
				})
				if err != nil {
					return nil, nil, err
				}

				return result.Ok[blob.AllocateOk, failure.IPLDBuilderFailure](
					blob.AllocateOk{
						Size:    resp.Size,
						Address: resp.Address,
					},
				), nil, nil
			},
		),
	)
}
