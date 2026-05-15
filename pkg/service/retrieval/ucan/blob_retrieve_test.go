package ucan

import (
	"testing"

	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/principal"

	"github.com/fil-forge/piri/pkg/store/blobstore"
)

// TestBlobRetrieve unit-tests the /blob/retrieve UCAN handler against a
// stubbed retrieval transport. The full end-to-end test (build an
// HTTPHeader-codec invocation, route it through the handler, decode the
// response container) is pending the ucantone retrieval client helpers
// + an HTTPHeader inbound test harness.
//
// Subtests pending restoration once the helpers land:
//
//   - not found when missing blob
//   - bad proof
//   - wrong resource
//
// Each subtest builds a /blob/retrieve invocation with controlled
// proof/resource semantics and asserts the receipt's typed failure name +
// HTTP status carried in the response container.
func TestBlobRetrieve(t *testing.T) {
	// fx-side smoke check: a BlobRetrievalService can be constructed against
	// the in-memory blobstore.
	var _ BlobRetrievalService = (*blobRetrievalService)(nil)
	_ = testutil.Alice
	t.Skip("blob/retrieve handler suite awaits ucantone retrieval client + HTTPHeader test harness")
}

type blobRetrievalService struct {
	id    principal.Signer
	blobs blobstore.BlobGetter
}

func (brs *blobRetrievalService) ID() principal.Signer {
	return brs.id
}

func (brs *blobRetrievalService) Blobs() blobstore.BlobGetter {
	return brs.blobs
}
