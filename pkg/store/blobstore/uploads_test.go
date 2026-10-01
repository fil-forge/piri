package blobstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/fil-forge/libforge/testutil"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store"
)

type mapIndex struct {
	mu sync.Mutex
	m  map[string]string
	// forgetErr fails the next Forget, as a crash before it would leave it.
	forgetErr error
}

func (i *mapIndex) Upload(_ context.Context, digest multihash.Multihash) (string, bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	id, ok := i.m[string(digest)]
	return id, ok, nil
}

func (i *mapIndex) Forget(_ context.Context, digest multihash.Multihash) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.forgetErr; err != nil {
		i.forgetErr = nil
		return err
	}
	delete(i.m, string(digest))
	return nil
}

func (i *mapIndex) hold(digest multihash.Multihash, id string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.m[string(digest)] = id
}

// uploaded stores data under upload id and records it as holding the digest.
func uploaded(t *testing.T) (Blobstore, Blobstore, *mapIndex, []byte, multihash.Multihash, string) {
	t.Helper()
	base := NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
	idx := &mapIndex{m: map[string]string{}}
	bs := WithUploads(base, idx)
	data := testutil.RandomBytes(t, 1024)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	id := "u1"
	require.NoError(t, bs.PutUpload(t.Context(), id, uint64(len(data)), bytes.NewReader(data)))
	idx.hold(digest, id)
	return bs, base, idx, data, digest, id
}

func readAll(t *testing.T, obj Object) []byte {
	t.Helper()
	b, err := io.ReadAll(obj.Body())
	require.NoError(t, err)
	return b
}

func TestUploads(t *testing.T) {
	ctx := context.Background()

	t.Run("reads a blob held by its upload", func(t *testing.T) {
		bs, base, _, data, digest, _ := uploaded(t)
		_, err := base.Get(ctx, digest)
		require.ErrorIs(t, err, store.ErrNotFound, "nothing is at the digest key")

		obj, err := bs.Get(ctx, digest)
		require.NoError(t, err)
		require.Equal(t, data, readAll(t, obj))

		end := uint64(9)
		obj, err = bs.Get(ctx, digest, WithRange(5, &end))
		require.NoError(t, err)
		require.Equal(t, data[5:10], readAll(t, obj), "ranges apply to the upload")
	})

	t.Run("a blob held by nothing is missing", func(t *testing.T) {
		bs, _, _, _, _, _ := uploaded(t)
		_, err := bs.Get(ctx, testutil.RandomMultihash(t))
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("settle moves the blob as it is read", func(t *testing.T) {
		bs, base, idx, data, digest, id := uploaded(t)
		var got []byte
		settled, err := bs.Settle(ctx, digest, func(r io.Reader, size int64) error {
			require.Equal(t, int64(len(data)), size)
			var err error
			got, err = io.ReadAll(r)
			return err
		})
		require.NoError(t, err)
		require.True(t, settled)
		require.Equal(t, data, got, "read got the blob")

		obj, err := base.Get(ctx, digest)
		require.NoError(t, err, "the blob is at its digest key")
		require.Equal(t, data, readAll(t, obj))
		_, err = base.GetUpload(ctx, id)
		require.ErrorIs(t, err, store.ErrNotFound, "the upload is gone")
		_, held, _ := idx.Upload(ctx, digest)
		require.False(t, held, "the index forgot it")

		obj, err = bs.Get(ctx, digest)
		require.NoError(t, err)
		require.Equal(t, data, readAll(t, obj))
	})

	t.Run("settle writes what read leaves unread", func(t *testing.T) {
		bs, base, _, data, digest, _ := uploaded(t)
		settled, err := bs.Settle(ctx, digest, func(r io.Reader, _ int64) error {
			_, err := io.ReadFull(r, make([]byte, 10))
			return err
		})
		require.NoError(t, err)
		require.True(t, settled)
		obj, err := base.Get(ctx, digest)
		require.NoError(t, err)
		require.Equal(t, data, readAll(t, obj))
	})

	t.Run("a failed read settles nothing", func(t *testing.T) {
		bs, _, idx, data, digest, id := uploaded(t)
		bad := errors.New("commp failed")
		_, err := bs.Settle(ctx, digest, func(io.Reader, int64) error { return bad })
		require.ErrorIs(t, err, bad)
		held, ok, _ := idx.Upload(ctx, digest)
		require.True(t, ok)
		require.Equal(t, id, held, "the upload still holds the blob")
		obj, err := bs.Get(ctx, digest)
		require.NoError(t, err)
		require.Equal(t, data, readAll(t, obj))
	})

	t.Run("settle reports false for a blob no upload holds", func(t *testing.T) {
		bs, _, _, _, _, _ := uploaded(t)
		settled, err := bs.Settle(ctx, testutil.RandomMultihash(t), func(io.Reader, int64) error {
			t.Fatal("read called for a blob no upload holds")
			return nil
		})
		require.NoError(t, err)
		require.False(t, settled)
	})

	t.Run("settle finishes an interrupted settle without reading", func(t *testing.T) {
		bs, base, idx, data, digest, id := uploaded(t)
		// A settle that wrote the digest key and stopped before cleaning up.
		require.NoError(t, base.Put(ctx, digest, uint64(len(data)), bytes.NewReader(data)))
		settled, err := bs.Settle(ctx, digest, func(io.Reader, int64) error {
			t.Fatal("read called for a blob already at its key")
			return nil
		})
		require.NoError(t, err)
		require.False(t, settled, "the caller reads it as any blob at its key")
		_, err = base.GetUpload(ctx, id)
		require.ErrorIs(t, err, store.ErrNotFound)
		_, held, _ := idx.Upload(ctx, digest)
		require.False(t, held)
	})

	t.Run("settle interrupted before forgetting the upload leaves nothing unreferenced", func(t *testing.T) {
		bs, base, idx, data, digest, id := uploaded(t)
		crash := errors.New("crashed")
		idx.forgetErr = crash
		_, err := bs.Settle(ctx, digest, func(r io.Reader, _ int64) error {
			_, err := io.Copy(io.Discard, r)
			return err
		})
		require.ErrorIs(t, err, crash)
		_, err = base.GetUpload(ctx, id)
		require.ErrorIs(t, err, store.ErrNotFound, "the upload went before its entry")
		held, ok, _ := idx.Upload(ctx, digest)
		require.True(t, ok, "the entry still records the cleanup owed")
		require.Equal(t, id, held)

		obj, err := bs.Get(ctx, digest)
		require.NoError(t, err, "the blob reads from its key while the entry remains")
		require.Equal(t, data, readAll(t, obj))

		settled, err := bs.Settle(ctx, digest, func(io.Reader, int64) error {
			t.Fatal("read called for a blob already at its key")
			return nil
		})
		require.NoError(t, err)
		require.False(t, settled)
		_, ok, _ = idx.Upload(ctx, digest)
		require.False(t, ok, "the retry forgot the upload")
		obj, err = bs.Get(ctx, digest)
		require.NoError(t, err)
		require.Equal(t, data, readAll(t, obj))
	})

	t.Run("delete removes a blob held by its upload", func(t *testing.T) {
		bs, base, idx, _, digest, id := uploaded(t)
		require.NoError(t, bs.Delete(ctx, digest))
		_, err := base.GetUpload(ctx, id)
		require.ErrorIs(t, err, store.ErrNotFound)
		_, held, _ := idx.Upload(ctx, digest)
		require.False(t, held)
		_, err = bs.Get(ctx, digest)
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("a store without an index never settles", func(t *testing.T) {
		base := NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
		settled, err := base.Settle(ctx, testutil.RandomMultihash(t), nil)
		require.NoError(t, err)
		require.False(t, settled)
	})
}
