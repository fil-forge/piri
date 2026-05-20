package blob

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/fil-forge/libforge/testutil"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/sync"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/pdp/piece"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

// Unit tests for the shared Retrieve function — exercises piece-store
// outcomes (found, not found, range out of bounds, partial range) and
// asserts the HTTP-shaped response container the byte-streaming handlers
// pass to rsp.SetMetadata. End-to-end HTTP coverage lives in
// pkg/ucanhandlers/ucanfxtest/retrieval; here we drive the function
// directly with no UCAN plumbing.

func newPieceReader(t *testing.T, data ...[]byte) (types.PieceReaderAPI, []multihash.Multihash) {
	t.Helper()
	blobs := blobstore.NewDatastoreStore(sync.MutexWrap(datastore.NewMapDatastore()))
	digests := make([]multihash.Multihash, len(data))
	for i, b := range data {
		digest, err := multihash.Sum(b, multihash.SHA2_256, -1)
		require.NoError(t, err)
		require.NoError(t, blobs.Put(t.Context(), digest, uint64(len(b)), bytes.NewReader(b)))
		digests[i] = digest
	}
	reader, err := piece.NewStoreReader(blobs)
	require.NoError(t, err)
	return reader, digests
}

func TestRetrieve_NotFound(t *testing.T) {
	reader, _ := newPieceReader(t)

	container, err := Retrieve(t.Context(), reader, testutil.RandomMultihash(t), nil)
	require.Error(t, err, "missing blob should surface as a named UCAN error")
	require.Equal(t, NotFoundErrorName, namedError(t, err))

	require.NotNil(t, container)
	require.Equal(t, http.StatusNotFound, container.StatusCode)
	require.Equal(t, NotFoundErrorName, container.Header.Get(ErrorNameHeader))
	body, ioErr := io.ReadAll(container.Body)
	require.NoError(t, ioErr)
	require.Contains(t, string(body), "blob not found")
}

func TestRetrieve_FullRead(t *testing.T) {
	data := testutil.RandomBytes(t, 256)
	reader, digests := newPieceReader(t, data)

	container, err := Retrieve(t.Context(), reader, digests[0], nil)
	require.NoError(t, err)
	defer container.Body.Close()

	require.Equal(t, http.StatusOK, container.StatusCode)
	require.Equal(t, strconv.Itoa(len(data)), container.Header.Get("Content-Length"))
	require.Empty(t, container.Header.Get("Content-Range"), "full read carries no Content-Range")

	body, ioErr := io.ReadAll(container.Body)
	require.NoError(t, ioErr)
	require.Equal(t, data, body)
}

func TestRetrieve_PartialRange(t *testing.T) {
	data := testutil.RandomBytes(t, 256)
	reader, digests := newPieceReader(t, data)
	end := uint64(7)

	container, err := Retrieve(t.Context(), reader, digests[0], &blobstore.Range{Start: 2, End: &end})
	require.NoError(t, err)
	defer container.Body.Close()

	require.Equal(t, http.StatusPartialContent, container.StatusCode)
	require.Equal(t, "6", container.Header.Get("Content-Length"), "end - start + 1 = 6")
	require.Equal(t, "bytes 2-7/256", container.Header.Get("Content-Range"))

	body, ioErr := io.ReadAll(container.Body)
	require.NoError(t, ioErr)
	require.Equal(t, data[2:8], body)
}

func TestRetrieve_RangeOutOfBounds(t *testing.T) {
	data := testutil.RandomBytes(t, 32)
	reader, digests := newPieceReader(t, data)
	end := uint64(64)

	container, err := Retrieve(t.Context(), reader, digests[0], &blobstore.Range{Start: 0, End: &end})
	require.Error(t, err)
	require.Equal(t, RangeNotSatisfiableErrorName, namedError(t, err))

	require.NotNil(t, container)
	require.Equal(t, http.StatusRequestedRangeNotSatisfiable, container.StatusCode)
	require.Equal(t, RangeNotSatisfiableErrorName, container.Header.Get(ErrorNameHeader))
}

// namedError extracts the stable error name from a UCAN error returned
// by Retrieve. The errors package's named type implements a Name()
// method via the embedded ErrorModel; we extract it via interface
// assertion to keep the test independent of the concrete error type.
func namedError(t *testing.T, err error) string {
	t.Helper()
	type named interface{ Name() string }
	n, ok := err.(named)
	require.True(t, ok, "expected named UCAN error, got %T", err)
	return n.Name()
}
