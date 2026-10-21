package blobstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/fil-forge/libforge/testutil"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/objectstore/dsadapter"
)

type mapIndex struct {
	mu sync.Mutex
	m  map[string]string
	// deleteErr fails the next Delete, as a crash before it would leave it.
	deleteErr error
}

func (i *mapIndex) GetID(_ context.Context, digest multihash.Multihash) (string, bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	id, ok := i.m[string(digest)]
	return id, ok, nil
}

func (i *mapIndex) Delete(_ context.Context, digest multihash.Multihash) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.deleteErr; err != nil {
		i.deleteErr = nil
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

// copyingBackend is a backend that cannot move objects, as the in-memory one
// cannot. pause, if set, runs at the start of the next Put.
type copyingBackend struct {
	objectstore.Store
	pause func()
}

func (b *copyingBackend) Put(ctx context.Context, key string, size uint64, data io.Reader) error {
	if f := b.pause; f != nil {
		b.pause = nil
		f()
	}
	return b.Store.Put(ctx, key, size, data)
}

// movingBackend is a backend that can move objects, as flatfs and MinIO can.
// pause, if set, runs at the start of the next Move.
type movingBackend struct {
	objectstore.Store
	pause func()
	moves int
}

func (b *movingBackend) Move(ctx context.Context, src, dst string) error {
	if f := b.pause; f != nil {
		b.pause = nil
		f()
	}
	b.moves++
	obj, err := b.Get(ctx, src)
	if errors.Is(err, objectstore.ErrNotExist) {
		if dstObj, err := b.Get(ctx, dst); err == nil {
			_ = dstObj.Body().Close()
			return nil
		}
		return objectstore.ErrNotExist
	}
	if err != nil {
		return err
	}
	body := obj.Body()
	defer body.Close()
	if err := b.Put(ctx, dst, uint64(obj.Size()), body); err != nil {
		return err
	}
	return b.Delete(ctx, src)
}

type stagingFixture struct {
	bs     *StagingStore
	base   *Store
	idx    *mapIndex
	data   []byte
	digest multihash.Multihash
	id     string
	// pause runs once, inside the next settle's write: the copy's Put, or the
	// Move.
	pause func(func())
	// moves counts the backend's moves; it stays 0 on a copying backend.
	moves func() int
}

// staged stages data under an upload id and records it as holding the digest,
// on a backend that moves objects or one that only copies them.
func staged(t *testing.T, move bool) stagingFixture {
	t.Helper()
	inner := dsadapter.New(dssync.MutexWrap(datastore.NewMapDatastore()))
	f := stagingFixture{idx: &mapIndex{m: map[string]string{}}, id: "u1"}
	var backend objectstore.Store
	if move {
		m := &movingBackend{Store: inner}
		backend = m
		f.pause = func(p func()) { m.pause = p }
		f.moves = func() int { return m.moves }
	} else {
		c := &copyingBackend{Store: inner}
		backend = c
		f.pause = func(p func()) { c.pause = p }
		f.moves = func() int { return 0 }
	}
	f.base = &Store{backend: backend, encoder: PlainKeyEncoder{}}
	f.bs = NewStagingStore(f.base, f.idx)
	f.data = testutil.RandomBytes(t, 1024)
	f.digest = testutil.Must(multihash.Sum(f.data, multihash.SHA2_256, -1))(t)
	require.NoError(t, f.bs.Stage(t.Context(), f.id, uint64(len(f.data)), bytes.NewReader(f.data)))
	f.idx.hold(f.digest, f.id)
	return f
}

func readAll(t *testing.T, obj Object) []byte {
	t.Helper()
	b, err := io.ReadAll(obj.Body())
	require.NoError(t, err)
	return b
}

func (f stagingFixture) requireSettled(t *testing.T) {
	t.Helper()
	obj, err := f.base.Get(t.Context(), f.digest)
	require.NoError(t, err, "the blob is at its digest key")
	require.Equal(t, f.data, readAll(t, obj))
	_, err = f.bs.GetStaged(t.Context(), f.id)
	require.ErrorIs(t, err, store.ErrNotFound, "the blob is unstaged")
	_, ok, _ := f.idx.GetID(t.Context(), f.digest)
	require.False(t, ok, "the entry is deleted")
}

func TestStagingStore(t *testing.T) {
	ctx := context.Background()

	t.Run("reads a staged blob", func(t *testing.T) {
		f := staged(t, false)
		_, err := f.base.Get(ctx, f.digest)
		require.ErrorIs(t, err, store.ErrNotFound, "nothing is at the digest key")

		obj, err := f.bs.Get(ctx, f.digest)
		require.NoError(t, err)
		require.Equal(t, f.data, readAll(t, obj))

		end := uint64(9)
		obj, err = f.bs.Get(ctx, f.digest, WithRange(5, &end))
		require.NoError(t, err)
		require.Equal(t, f.data[5:10], readAll(t, obj), "ranges apply to the staged blob")
	})

	t.Run("a blob neither stored nor staged is missing", func(t *testing.T) {
		f := staged(t, false)
		_, err := f.bs.Get(ctx, testutil.RandomMultihash(t))
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("delete removes a staged blob", func(t *testing.T) {
		f := staged(t, false)
		require.NoError(t, f.bs.Delete(ctx, f.digest))
		_, err := f.bs.GetStaged(ctx, f.id)
		require.ErrorIs(t, err, store.ErrNotFound)
		_, held, _ := f.idx.GetID(ctx, f.digest)
		require.False(t, held)
		_, err = f.bs.Get(ctx, f.digest)
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	for _, move := range []bool{false, true} {
		name := "copying backend"
		if move {
			name = "moving backend"
		}
		t.Run(name, func(t *testing.T) { testSettle(t, move) })
	}
}

func testSettle(t *testing.T, move bool) {
	ctx := context.Background()

	t.Run("settle puts the blob at its key", func(t *testing.T) {
		f := staged(t, move)
		require.NoError(t, f.bs.Settle(ctx, f.digest))
		f.requireSettled(t)
		if move {
			require.Equal(t, 1, f.moves(), "the backend moves the blob")
		}
		obj, err := f.bs.Get(ctx, f.digest)
		require.NoError(t, err)
		require.Equal(t, f.data, readAll(t, obj))
	})

	t.Run("settling a blob that is not staged does nothing", func(t *testing.T) {
		f := staged(t, move)
		require.NoError(t, f.bs.Settle(ctx, testutil.RandomMultihash(t)))
		require.Zero(t, f.moves())
	})

	t.Run("settle finishes a settle interrupted after the write", func(t *testing.T) {
		f := staged(t, move)
		// The blob reached its key, and the settle stopped before the rest.
		require.NoError(t, f.base.Put(ctx, f.digest, uint64(len(f.data)), bytes.NewReader(f.data)))
		require.NoError(t, f.bs.Settle(ctx, f.digest))
		f.requireSettled(t)
	})

	t.Run("settle finishes a settle interrupted before deleting the entry", func(t *testing.T) {
		f := staged(t, move)
		crash := errors.New("crashed")
		f.idx.deleteErr = crash
		require.ErrorIs(t, f.bs.Settle(ctx, f.digest), crash)
		_, err := f.bs.GetStaged(ctx, f.id)
		require.ErrorIs(t, err, store.ErrNotFound, "the blob was unstaged before its entry was dropped")
		_, ok, _ := f.idx.GetID(ctx, f.digest)
		require.True(t, ok, "the entry still records the settle owed")

		obj, err := f.bs.Get(ctx, f.digest)
		require.NoError(t, err, "the blob reads from its key while the entry remains")
		require.Equal(t, f.data, readAll(t, obj))

		require.NoError(t, f.bs.Settle(ctx, f.digest))
		f.requireSettled(t)
	})

	t.Run("a delete during a settle leaves nothing at the digest key", func(t *testing.T) {
		f := staged(t, move)
		deleted := make(chan error, 1)
		f.pause(func() {
			// The removal sweep deletes the blob while it is being settled.
			go func() { deleted <- f.bs.Delete(ctx, f.digest) }()
			select {
			case err := <-deleted:
				deleted <- err
			case <-time.After(100 * time.Millisecond):
			}
		})
		require.NoError(t, f.bs.Settle(ctx, f.digest))
		require.NoError(t, <-deleted)

		_, err := f.base.Get(ctx, f.digest)
		require.ErrorIs(t, err, store.ErrNotFound, "no copy of the removed blob is left at its key")
		_, err = f.bs.GetStaged(ctx, f.id)
		require.ErrorIs(t, err, store.ErrNotFound)
		_, held, _ := f.idx.GetID(ctx, f.digest)
		require.False(t, held)
	})
}
