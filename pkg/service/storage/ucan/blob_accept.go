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

func WithBlobAcceptMethod(deps blobhandler.AcceptDeps) server.Option {
	return server.WithServiceMethod(
		blob.AcceptAbility,
		server.Provide(
			blob.Accept,
			func(ctx context.Context, cap ucan.Capability[blob.AcceptCaveats], inv invocation.Invocation, iCtx server.InvocationContext) (result.Result[blob.AcceptOk, failure.IPLDBuilderFailure], fx.Effects, error) {
				//
				// UCAN Validation
				//

				// only service principal can perform an allocation
				if cap.With() != iCtx.ID().DID().String() {
					return result.Error[blob.AcceptOk, failure.IPLDBuilderFailure](NewUnsupportedCapabilityError(cap)), nil, nil
				}

				//
				// end UCAN Validation
				//

				resp, err := blobhandler.Accept(ctx, deps, &blobhandler.AcceptRequest{
					Space: cap.Nb().Space,
					Blob:  cap.Nb().Blob,
					Put:   cap.Nb().Put,
					Cause: inv.Link(),
				})
				if err != nil {
					return nil, nil, err
				}
				forks := []fx.Effect{fx.FromInvocation(resp.Claim)}
				res := blob.AcceptOk{
					Site: resp.Claim.Link(),
				}
				if resp.PDP != nil {
					forks = append(forks, fx.FromInvocation(resp.PDP))
					tmp := resp.PDP.Link()
					res.PDP = &tmp
				}

				return result.Ok[blob.AcceptOk, failure.IPLDBuilderFailure](res), fx.NewEffects(fx.WithFork(forks...)), nil
			},
		),
	)
}
