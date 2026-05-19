package ucan

import (
	"github.com/fil-forge/libforge/capabilities/blob"
	"github.com/fil-forge/ucantone/execution/bindexec"

	blobhandler "github.com/fil-forge/piri/pkg/service/storage/handlers/blob"
)

// InternalErrorName is the stable receipt-failure name for invariant
// violations the handler hits at runtime (e.g., a link type we expected
// to be a cidlink wasn't).
const InternalErrorName = "InternalError"

func NewBlobAcceptHandler(deps blobhandler.AcceptDeps) Handler {
	return TypedHandler(
		blob.Accept,
		func(req *bindexec.Request[*blob.AcceptArguments], rsp *bindexec.Response[*blob.AcceptOK]) error {
			args := req.Task().Arguments()

			if err := requireSubject(req, deps.ID.DID()); err != nil {
				return rsp.SetFailure(err)
			}

			resp, err := blobhandler.Accept(req.Context(), deps, &blobhandler.AcceptRequest{
				Space: req.Task().Subject(),
				Blob: blob.Blob{
					Digest: args.Blob.Digest,
					Size:   args.Blob.Size,
				},
				// TODO(forrest)[ucan1]: need to double check this..
				Put:   args.Put,
				Cause: req.Invocation().Link(),
			})
			if err != nil {
				return err
			}

			// TODO(forrest)[ucan1]: attach resp.Claim and resp.PDP as
			// response container metadata once Phase 5b migrates the
			// location-claim and pdp/accept invocations to ucantone. The
			// legacy path returned fx.NewEffects(fx.WithFork(
			//     fx.FromInvocation(resp.Claim),
			//     fx.FromInvocation(resp.PDP))).

			return rsp.SetSuccess(&blob.AcceptOK{Site: resp.Claim.Link()})
		},
	)
}

/*
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
				pdpLink := resp.PDP.Link()
				return result.Ok[blob.AcceptOk, failure.IPLDBuilderFailure](blob.AcceptOk{
					Site: resp.Claim.Link(),
					PDP:  &pdpLink,
				}), fx.NewEffects(fx.WithFork(fx.FromInvocation(resp.Claim), fx.FromInvocation(resp.PDP))), nil
			},
		),
	)
}

*/
