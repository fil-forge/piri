package ucan

import (
	"github.com/fil-forge/libforge/capabilities/content"
	"github.com/fil-forge/ucantone/execution/bindexec"
	logging "github.com/ipfs/go-log/v2"
	fxlib "go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
)

var log = logging.Logger("retrieval/ucan")

// SpaceContentRetrieveDeps is the dependency set populated by fx for the
// space/content/retrieve UCAN method.
type SpaceContentRetrieveDeps struct {
	fxlib.In
	Allocations allocationstore.AllocationStore
	Pieces      types.PieceReaderAPI
}

func NewSpaceContentRetrieveHandler(deps SpaceContentRetrieveDeps) Handler {
	return TypedHandler(
		content.Retrieve,
		func(req *bindexec.Request[*content.RetrieveArguments], rsp *bindexec.Response[*content.RetrieveOK]) error {
			return nil
		},
	)

}

/*
func WithSpaceContentRetrieveMethod(deps SpaceContentRetrieveDeps) retrieval.Option {
	return retrieval.WithServiceMethod(
		content.RetrieveAbility,
		retrieval.Provide(
			content.Retrieve,
			func(ctx context.Context, cap ucan.Capability[content.RetrieveCaveats], inv invocation.Invocation, iCtx server.InvocationContext, request retrieval.Request) (res result.Result[content.RetrieveOk, failure.IPLDBuilderFailure], effects fx.Effects, resp retrieval.Response, err error) {
				ctx, span := tracer.Start(ctx, "space.content.retrieve")
				defer func() {
					if err != nil {
						span.RecordError(err)
						span.SetStatus(codes.Error, err.Error())
					}
					span.End()
				}()

				space, err := did.Parse(cap.With())
				if err != nil {
					return nil, nil, retrieval.Response{}, fmt.Errorf("parsing space DID: %w", err)
				}

				nb := cap.Nb()
				digest := nb.Blob.Digest
				digestStr := digestutil.Format(digest)
				start := nb.Range.Start
				end := nb.Range.End

				attr := []attribute.KeyValue{
					attribute.String("space.did", space.String()),
					attribute.String("digest", digestStr),
					attribute.Int64("range.start", int64(start)),
					attribute.Int64("range.end", int64(end)),
					attribute.String("issuer", inv.Issuer().DID().String()),
				}
				span.SetAttributes(attr...)

				log := log.With(
					"iss", inv.Issuer().DID().String(),
					"can", content.RetrieveAbility,
					"with", space.String(),
					"digest", digestStr,
					"range", fmt.Sprintf("%d-%d", start, end),
				)

				_, err = deps.Allocations.Get(ctx, digest, space)
				if err != nil {
					if errors.Is(err, store.ErrNotFound) {
						log.Debugw("allocation not found", "status", http.StatusNotFound)
						notFoundErr := content.NewNotFoundError(fmt.Sprintf("allocation not found: %s", digestStr))
						res := result.Error[content.RetrieveOk, failure.IPLDBuilderFailure](notFoundErr)
						resp := retrieval.NewResponse(http.StatusNotFound, nil, nil)
						return res, nil, resp, nil
					}
					log.Errorw("getting allocation", "error", err)
					return nil, nil, retrieval.Response{}, fmt.Errorf("getting allocation: %w", err)
				}

				res, resp, err = spacecontent.Retrieve(ctx, deps.Pieces, inv, digest, &blobstore.Range{Start: start, End: &end})
				if err != nil {
					return nil, nil, retrieval.Response{}, err
				}
				return res, nil, resp, nil
			},
		),
	)
}

*/
