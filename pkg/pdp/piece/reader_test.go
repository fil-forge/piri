package piece

import (
	"bytes"
	"testing"

	"github.com/fil-forge/libforge/testutil"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store/blobstore"
)

// Has reflects the store as it is now: a removed blob is not reported held.
func TestStoreReaderHasFollowsTheStore(t *testing.T) {
	ctx := t.Context()
	bs := blobstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
	r, err := NewStoreReader(bs)
	require.NoError(t, err)
	data := testutil.RandomBytes(t, 32)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)

	require.NoError(t, bs.Put(ctx, digest, uint64(len(data)), bytes.NewReader(data)))
	has, err := r.Has(ctx, digest)
	require.NoError(t, err)
	require.True(t, has)

	require.NoError(t, bs.Delete(ctx, digest))
	has, err = r.Has(ctx, digest)
	require.NoError(t, err)
	require.False(t, has, "a removed blob is not reported held")
}
