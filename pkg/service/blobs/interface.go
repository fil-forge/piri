package blobs

import (
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
)

type Blobs interface {
	// Allocations is a store for received blob allocations.
	Allocations() allocationstore.AllocationStore
	// Acceptances is a store that records accepted blobs.
	Acceptances() acceptancestore.AcceptanceStore
}
