package blobs

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/service/blobs"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
)

var Module = fx.Module("blobs",
	fx.Provide(
		NewService,
		// Also provide the interface
		fx.Annotate(
			func(svc *blobs.BlobService) blobs.Blobs {
				return svc
			},
		),
	),
)

type NewServiceParams struct {
	fx.In

	AllocationStore allocationstore.AllocationStore
	AcceptanceStore acceptancestore.AcceptanceStore
}

func NewService(params NewServiceParams) (*blobs.BlobService, error) {
	return blobs.New(
		blobs.WithAllocationStore(params.AllocationStore),
		blobs.WithAcceptanceStore(params.AcceptanceStore),
	)
}
