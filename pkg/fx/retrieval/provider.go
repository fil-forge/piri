package retrieval

import (
	"github.com/fil-forge/go-ucanto/principal"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/pdp/store/adapter"
	"github.com/fil-forge/piri/pkg/service/retrieval"
	"github.com/fil-forge/piri/pkg/service/retrieval/ucan"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

var Module = fx.Module("retrieval",
	fx.Provide(
		fx.Annotate(
			NewRetrievalService,
			fx.As(new(ucan.BlobRetrievalService)),
			fx.As(new(ucan.SpaceContentRetrievalService)),
		),
	),
)

// RetrievalServiceParams contains all dependencies for the retrieval service
type RetrievalServiceParams struct {
	fx.In

	ID          principal.Signer
	Allocations allocationstore.AllocationStore
	Blobs       blobstore.BlobGetter
	API         types.PieceReaderAPI `optional:"true"`
}

func NewRetrievalService(params RetrievalServiceParams) *retrieval.RetrievalService {
	blobs := params.Blobs
	// When PDP is enabled, blobs are stored in the piece store and keyed by piece
	// hash. We need to adapt it to resolve a blob hash to a piece hash before
	// fetching.
	if params.API != nil {
		blobs = adapter.NewBlobGetterAdapter(params.API)
	}
	return retrieval.New(params.ID, blobs, params.Allocations)
}
