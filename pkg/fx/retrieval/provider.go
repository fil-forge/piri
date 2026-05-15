package retrieval

import (
	"github.com/fil-forge/go-ucanto/principal"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/pdp/store/adapter"
	"github.com/fil-forge/piri/pkg/service/retrieval"
	"github.com/fil-forge/piri/pkg/service/retrieval/ucan"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
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
	API         types.PieceReaderAPI
}

func NewRetrievalService(params RetrievalServiceParams) *retrieval.RetrievalService {
	// Bytes live in the PDP piece store; the adapter exposes them as a
	// BlobGetter keyed by user hash.
	return retrieval.New(params.ID, adapter.NewBlobGetterAdapter(params.API), params.Allocations)
}
