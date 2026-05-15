package blobs

import (
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
)

type BlobService struct {
	*options
}

func (b *BlobService) Allocations() allocationstore.AllocationStore {
	return b.allocStore
}

func (b *BlobService) Acceptances() acceptancestore.AcceptanceStore {
	return b.acceptStore
}

var _ Blobs = (*BlobService)(nil)

func New(opts ...Option) (*BlobService, error) {
	o := &options{}
	for _, opt := range opts {
		err := opt(o)
		if err != nil {
			return nil, err
		}
	}

	return &BlobService{o}, nil
}
