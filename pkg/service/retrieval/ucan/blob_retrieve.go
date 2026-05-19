package ucan

import (
	"github.com/fil-forge/libforge/capabilities/blob"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/principal"
	fxlib "go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/pdp/types"
)

// InvalidResourceErrorName is the name given to an error where the resource did
// not match the service DID.
const InvalidResourceErrorName = "InvalidResource"

// BlobRetrieveDeps is the dependency set populated by fx for the
// blob/retrieve UCAN method.
type BlobRetrieveDeps struct {
	fxlib.In
	ID     principal.Signer
	Pieces types.PieceReaderAPI
}

func NewBlobRetrieveHandler(deps BlobRetrieveDeps) Handler {
	return TypedHandler(
		blob.Retrieve,
		func(req *bindexec.Request[*blob.RetrieveArguments], rsp *bindexec.Response[*blob.RetrieveOK]) error {
			return nil
		},
	)

}

/*
func WithBlobRetrieveMethod(deps BlobRetrieveDeps) retrieval.Option {
	return retrieval.WithServiceMethod(
		blob.RetrieveAbility,
		retrieval.Provide(
			blob.Retrieve,
			func(ctx context.Context, cap ucan.Capability[blob.RetrieveCaveats], inv invocation.Invocation, iCtx server.InvocationContext, request retrieval.Request) (result.Result[blob.RetrieveOk, failure.IPLDBuilderFailure], fx.Effects, retrieval.Response, error) {
				if cap.With() != deps.ID.DID().String() {
					return result.Error[blob.RetrieveOk, failure.IPLDBuilderFailure](blob.RetrieveError{
						ErrorName: InvalidResourceErrorName,
						Message:   fmt.Sprintf("resource is %s not %s", cap.With(), deps.ID.DID()),
					}), nil, retrieval.Response{}, nil
				}
				// no range, pass nil for byteRange
				res, resp, err := spacecontent.Retrieve(ctx, deps.Pieces, inv, cap.Nb().Blob.Digest, nil)
				if err != nil {
					return nil, nil, retrieval.Response{}, err
				}
				return result.MapOk(res, func(o content.RetrieveOk) blob.RetrieveOk {
					return blob.RetrieveOk{}
				}), nil, resp, nil
			},
		),
	)
}


*/
