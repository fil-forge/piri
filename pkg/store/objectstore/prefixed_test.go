package objectstore_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/objectstore/dsadapter"
)

func TestPrefixed(t *testing.T) {
	ctx := context.Background()
	data := []byte("hello")

	t.Run("Put writes under the prefixed key", func(t *testing.T) {
		inner := dsadapter.New(dssync.MutexWrap(datastore.NewMapDatastore()))
		store := objectstore.Prefixed("ipni/", inner)
		require.NoError(t, store.Put(ctx, "k", uint64(len(data)), bytes.NewReader(data)))
		require.Equal(t, data, readObject(t, inner, "ipni/k"))
	})

	t.Run("Get reads the prefixed key", func(t *testing.T) {
		inner := dsadapter.New(dssync.MutexWrap(datastore.NewMapDatastore()))
		require.NoError(t, inner.Put(ctx, "ipni/k", uint64(len(data)), bytes.NewReader(data)))
		store := objectstore.Prefixed("ipni/", inner)
		require.Equal(t, data, readObject(t, store, "k"))
	})

	t.Run("Get does not see keys outside the prefix", func(t *testing.T) {
		inner := dsadapter.New(dssync.MutexWrap(datastore.NewMapDatastore()))
		require.NoError(t, inner.Put(ctx, "k", uint64(len(data)), bytes.NewReader(data)))
		store := objectstore.Prefixed("ipni/", inner)
		_, err := store.Get(ctx, "k")
		require.ErrorIs(t, err, objectstore.ErrNotExist)
	})

	t.Run("Delete removes the prefixed key", func(t *testing.T) {
		inner := dsadapter.New(dssync.MutexWrap(datastore.NewMapDatastore()))
		require.NoError(t, inner.Put(ctx, "ipni/k", uint64(len(data)), bytes.NewReader(data)))
		store := objectstore.Prefixed("ipni/", inner)
		require.NoError(t, store.Delete(ctx, "k"))
		_, err := inner.Get(ctx, "ipni/k")
		require.ErrorIs(t, err, objectstore.ErrNotExist)
	})
}

func readObject(t *testing.T, store objectstore.Store, key string) []byte {
	t.Helper()
	obj, err := store.Get(context.Background(), key)
	require.NoError(t, err)
	got, err := io.ReadAll(obj.Body())
	require.NoError(t, err)
	return got
}
