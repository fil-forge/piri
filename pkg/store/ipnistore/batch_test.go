package ipnistore

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/testutil"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/objectstore/memory"
)

func TestPutMany(t *testing.T) {
	ctx := t.Context()

	t.Run("writes every block", func(t *testing.T) {
		blocks := &blockStore{objects: memory.NewStore()}
		batch := []store.Block{
			{Key: testutil.RandomCID(t).String(), Data: []byte("one")},
			{Key: testutil.RandomCID(t).String(), Data: []byte("two")},
		}
		require.NoError(t, blocks.PutMany(ctx, batch))
		require.Equal(t, []string{"one", "two"}, []string{readAll(t, blocks, batch[0].Key), readAll(t, blocks, batch[1].Key)})
	})

	t.Run("a failed write fails the batch", func(t *testing.T) {
		blocks := &blockStore{objects: failingPuts{memory.NewStore()}}
		err := blocks.PutMany(ctx, []store.Block{{Key: testutil.RandomCID(t).String(), Data: []byte("one")}})
		require.ErrorIs(t, err, errPutFailed)
	})

	t.Run("the head is refused", func(t *testing.T) {
		blocks := &blockStore{objects: memory.NewStore()}
		err := blocks.PutMany(ctx, []store.Block{{Key: headKey, Data: []byte("head")}})
		require.ErrorContains(t, err, "written with Replace")
	})
}

func TestContextsMany(t *testing.T) {
	ctx := t.Context()
	one, two, absent := []byte("one"), []byte("two"), []byte("absent")

	t.Run("GetMany returns the mapped context IDs only", func(t *testing.T) {
		db, provider := newDB(t)
		chunks := &contextTable{db: db, provider: provider, column: chunkLinkColumn}
		require.NoError(t, chunks.PutMany(ctx, provider, []store.ContextEntry{{ContextID: one, Data: []byte("a")}, {ContextID: two, Data: []byte("b")}}))
		got, err := chunks.GetMany(ctx, provider, [][]byte{one, two, absent})
		require.NoError(t, err)
		require.Equal(t, map[string][]byte{"one": []byte("a"), "two": []byte("b")}, got)
	})

	t.Run("GetMany ignores the other map", func(t *testing.T) {
		db, provider := newDB(t)
		chunks := &contextTable{db: db, provider: provider, column: chunkLinkColumn}
		meta := &contextTable{db: db, provider: provider, column: metadataColumn}
		require.NoError(t, meta.PutMany(ctx, provider, []store.ContextEntry{{ContextID: one, Data: []byte("m")}}))
		got, err := chunks.GetMany(ctx, provider, [][]byte{one})
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("PutMany replaces existing mappings", func(t *testing.T) {
		db, provider := newDB(t)
		chunks := &contextTable{db: db, provider: provider, column: chunkLinkColumn}
		require.NoError(t, chunks.PutMany(ctx, provider, []store.ContextEntry{{ContextID: one, Data: []byte("a")}}))
		require.NoError(t, chunks.PutMany(ctx, provider, []store.ContextEntry{{ContextID: one, Data: []byte("b")}}))
		got, err := chunks.GetMany(ctx, provider, [][]byte{one})
		require.NoError(t, err)
		require.Equal(t, map[string][]byte{"one": []byte("b")}, got)
	})

	t.Run("DeleteMany removes rows with no mapping left", func(t *testing.T) {
		db, provider := newDB(t)
		chunks := &contextTable{db: db, provider: provider, column: chunkLinkColumn}
		require.NoError(t, chunks.PutMany(ctx, provider, []store.ContextEntry{{ContextID: one, Data: []byte("a")}, {ContextID: two, Data: []byte("b")}}))
		require.NoError(t, chunks.DeleteMany(ctx, provider, [][]byte{one, two}))
		require.Zero(t, countContexts(t, db))
	})

	t.Run("DeleteMany keeps the other map", func(t *testing.T) {
		db, provider := newDB(t)
		chunks := &contextTable{db: db, provider: provider, column: chunkLinkColumn}
		meta := &contextTable{db: db, provider: provider, column: metadataColumn}
		require.NoError(t, chunks.PutMany(ctx, provider, []store.ContextEntry{{ContextID: one, Data: []byte("a")}}))
		require.NoError(t, meta.PutMany(ctx, provider, []store.ContextEntry{{ContextID: one, Data: []byte("m")}}))
		require.NoError(t, chunks.DeleteMany(ctx, provider, [][]byte{one}))
		got, err := meta.GetMany(ctx, provider, [][]byte{one})
		require.NoError(t, err)
		require.Equal(t, map[string][]byte{"one": []byte("m")}, got)
	})
}

var errPutFailed = errors.New("put failed")

type failingPuts struct {
	objectstore.Store
}

func (failingPuts) Put(context.Context, string, uint64, io.Reader) error {
	return errPutFailed
}
