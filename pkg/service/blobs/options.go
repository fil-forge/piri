package blobs

import (
	"github.com/ipfs/go-datastore"
	logging "github.com/ipfs/go-log/v2"

	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
)

type options struct {
	allocStore  allocationstore.AllocationStore
	acceptStore acceptancestore.AcceptanceStore
}

type Option func(*options) error

// WithLogLevel changes the log level for the blobs subsystem.
func WithLogLevel(level string) Option {
	return func(o *options) error {
		logging.SetLogLevel("blobs", level)
		return nil
	}
}

func WithAllocationStore(allocationStore allocationstore.AllocationStore) Option {
	return func(o *options) error {
		o.allocStore = allocationStore
		return nil
	}
}

func WithDSAllocationStore(allocsDatastore datastore.Datastore) Option {
	return func(o *options) error {
		o.allocStore = allocationstore.NewDatastoreStore(allocsDatastore)
		return nil
	}
}

func WithAcceptanceStore(acceptanceStore acceptancestore.AcceptanceStore) Option {
	return func(o *options) error {
		o.acceptStore = acceptanceStore
		return nil
	}
}
