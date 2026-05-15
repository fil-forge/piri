package ucan

import (
	"testing"

	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

// TestSpaceContentRetrieve unit-tests the /content/retrieve UCAN handler.
// The full end-to-end test (build an HTTPHeader-codec invocation, route
// it through the handler, decode the response container, validate the
// receipt's typed failure for missing allocations / out-of-range / wrong
// space) is pending the ucantone retrieval client helpers + an HTTPHeader
// inbound test harness.
//
// Subtests pending restoration once helpers land:
//
//   - happy path full read
//   - happy path partial range
//   - missing allocation
//   - allocation in different space
//   - bad proof / unauthorized
//   - byte range not satisfiable
//   - blob not found in store
//
// Each subtest seeds the allocation + blob stores, builds a /content/retrieve
// invocation against the space subject, and asserts on the receipt + HTTP
// response container metadata.
func TestSpaceContentRetrieve(t *testing.T) {
	var _ SpaceContentRetrievalService = (*retrievalService)(nil)
	t.Skip("space/content/retrieve handler suite awaits ucantone retrieval client + HTTPHeader test harness")
}

type retrievalService struct {
	allocations allocationstore.AllocationStore
	blobs       blobstore.BlobGetter
}

func (rs *retrievalService) Allocations() allocationstore.AllocationStore {
	return rs.allocations
}

func (rs *retrievalService) Blobs() blobstore.BlobGetter {
	return rs.blobs
}
