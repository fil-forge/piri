package ipnistore

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/multikey"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/lib"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/store/objectstore/memory"
)

func TestHead(t *testing.T) {
	ctx := t.Context()

	t.Run("is not found before the first replace", func(t *testing.T) {
		blocks := newBlockStore(t)
		_, err := blocks.Get(ctx, headKey)
		require.True(t, store.IsNotFound(err), "got %v", err)
	})

	t.Run("the first replace writes it", func(t *testing.T) {
		blocks := newBlockStore(t)
		require.NoError(t, blocks.Replace(ctx, headKey, nil, 1, strings.NewReader("a")))
		require.Equal(t, "a", readAll(t, blocks, headKey))
	})

	t.Run("a replace from the current head moves it", func(t *testing.T) {
		blocks := newBlockStore(t)
		require.NoError(t, blocks.Replace(ctx, headKey, nil, 1, strings.NewReader("a")))
		require.NoError(t, blocks.Replace(ctx, headKey, strings.NewReader("a"), 1, strings.NewReader("b")))
		require.Equal(t, "b", readAll(t, blocks, headKey))
	})

	staleCases := map[string]io.Reader{
		"from a head that was replaced": strings.NewReader("a"),
		"as the first head":             nil,
	}
	for desc, old := range staleCases {
		t.Run("a replace "+desc+" fails", func(t *testing.T) {
			blocks := newBlockStore(t)
			require.NoError(t, blocks.Replace(ctx, headKey, nil, 1, strings.NewReader("a")))
			require.NoError(t, blocks.Replace(ctx, headKey, strings.NewReader("a"), 1, strings.NewReader("b")))
			err := blocks.Replace(ctx, headKey, old, 1, strings.NewReader("c"))
			require.ErrorIs(t, err, store.ErrPreconditionFailed)
		})
	}

	t.Run("cannot be written with Put", func(t *testing.T) {
		blocks := newBlockStore(t)
		require.Error(t, blocks.Put(ctx, headKey, 1, strings.NewReader("a")))
	})
}

func TestBlocks(t *testing.T) {
	ctx := t.Context()

	t.Run("round trip through the object store", func(t *testing.T) {
		blocks := newBlockStore(t)
		key := testutil.RandomCID(t).String()
		require.NoError(t, blocks.Put(ctx, key, 4, strings.NewReader("data")))
		require.Equal(t, "data", readAll(t, blocks, key))
	})

	t.Run("a missing block is not found", func(t *testing.T) {
		blocks := newBlockStore(t)
		_, err := blocks.Get(ctx, testutil.RandomCID(t).String())
		require.True(t, store.IsNotFound(err), "got %v", err)
	})

	t.Run("only the head can be replaced", func(t *testing.T) {
		blocks := newBlockStore(t)
		err := blocks.Replace(ctx, testutil.RandomCID(t).String(), nil, 1, strings.NewReader("a"))
		require.ErrorContains(t, err, "only the IPNI head")
	})
}

func TestContexts(t *testing.T) {
	ctx := t.Context()
	contextID := []byte("context")

	t.Run("a mapping reads back", func(t *testing.T) {
		db, provider := newDB(t)
		chunks := &contextTable{db: db, provider: provider, column: chunkLinkColumn}
		require.NoError(t, chunks.Put(ctx, provider, contextID, []byte("chunk")))
		got, err := chunks.Get(ctx, provider, contextID)
		require.NoError(t, err)
		require.Equal(t, []byte("chunk"), got)
	})

	t.Run("a put replaces the mapping", func(t *testing.T) {
		db, provider := newDB(t)
		chunks := &contextTable{db: db, provider: provider, column: chunkLinkColumn}
		require.NoError(t, chunks.Put(ctx, provider, contextID, []byte("chunk")))
		require.NoError(t, chunks.Put(ctx, provider, contextID, []byte("other")))
		got, err := chunks.Get(ctx, provider, contextID)
		require.NoError(t, err)
		require.Equal(t, []byte("other"), got)
	})

	t.Run("the two maps are independent", func(t *testing.T) {
		db, provider := newDB(t)
		chunks := &contextTable{db: db, provider: provider, column: chunkLinkColumn}
		meta := &contextTable{db: db, provider: provider, column: metadataColumn}
		require.NoError(t, chunks.Put(ctx, provider, contextID, []byte("chunk")))
		_, err := meta.Get(ctx, provider, contextID)
		require.True(t, store.IsNotFound(err), "got %v", err)
	})

	t.Run("a delete clears only its own map", func(t *testing.T) {
		db, provider := newDB(t)
		chunks := &contextTable{db: db, provider: provider, column: chunkLinkColumn}
		meta := &contextTable{db: db, provider: provider, column: metadataColumn}
		require.NoError(t, chunks.Put(ctx, provider, contextID, []byte("chunk")))
		require.NoError(t, meta.Put(ctx, provider, contextID, []byte("meta")))
		require.NoError(t, chunks.Delete(ctx, provider, contextID))
		got, err := meta.Get(ctx, provider, contextID)
		require.NoError(t, err)
		require.Equal(t, []byte("meta"), got)
	})

	t.Run("deleting both maps removes the row", func(t *testing.T) {
		db, provider := newDB(t)
		chunks := &contextTable{db: db, provider: provider, column: chunkLinkColumn}
		meta := &contextTable{db: db, provider: provider, column: metadataColumn}
		require.NoError(t, chunks.Put(ctx, provider, contextID, []byte("chunk")))
		require.NoError(t, meta.Put(ctx, provider, contextID, []byte("meta")))
		require.NoError(t, chunks.Delete(ctx, provider, contextID))
		require.NoError(t, meta.Delete(ctx, provider, contextID))
		require.Zero(t, countContexts(t, db))
	})

	t.Run("deleting a missing mapping succeeds", func(t *testing.T) {
		db, provider := newDB(t)
		chunks := &contextTable{db: db, provider: provider, column: chunkLinkColumn}
		require.NoError(t, chunks.Delete(ctx, provider, contextID))
	})

	t.Run("another provider is refused", func(t *testing.T) {
		db, provider := newDB(t)
		chunks := &contextTable{db: db, provider: provider, column: chunkLinkColumn}
		err := chunks.Put(ctx, peerOf(t, testutil.Bob), contextID, []byte("chunk"))
		require.ErrorContains(t, err, "holds provider")
	})
}

func newDB(t *testing.T) (*harmonydb.DB, peer.ID) {
	t.Helper()
	return piritestutil.NewHarmonyDB(t), peerOf(t, testutil.Alice)
}

func newBlockStore(t *testing.T) *blockStore {
	t.Helper()
	db, _ := newDB(t)
	return &blockStore{db: db, objects: memory.NewStore()}
}

func peerOf(t *testing.T, id multikey.Issuer) peer.ID {
	t.Helper()
	key, err := lib.Libp2pKey(id)
	require.NoError(t, err)
	p, err := peer.IDFromPrivateKey(key)
	require.NoError(t, err)
	return p
}

func readAll(t *testing.T, blocks *blockStore, key string) string {
	t.Helper()
	r, err := blocks.Get(context.Background(), key)
	require.NoError(t, err)
	defer r.Close()
	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	return buf.String()
}

func countContexts(t *testing.T, db *harmonydb.DB) int {
	t.Helper()
	var rows []struct {
		N int `db:"n"`
	}
	require.NoError(t, db.Select(context.Background(), &rows, `SELECT count(*) AS n FROM ipni_contexts`))
	return rows[0].N
}
