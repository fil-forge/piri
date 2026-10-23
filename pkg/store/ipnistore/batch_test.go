package ipnistore

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/fil-forge/go-ipni-tools/pkg/metadata"
	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/testutil"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/dagsync/ipnisync/head"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/lib"
	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/objectstore/memory"
)

func TestLoadAndCommitContexts(t *testing.T) {
	ctx := t.Context()
	one, two, absent := []byte("one"), []byte("two"), []byte("absent")

	t.Run("a committed record loads back", func(t *testing.T) {
		st := newStore(t)
		rec := record(t)
		commit(t, st, nil, store.ContextChange{ContextID: one, New: rec})
		got, err := st.LoadContexts(ctx, st.provider, [][]byte{one})
		require.NoError(t, err)
		requireRecords(t, map[string]store.ContextRecord{"one": rec}, got)
	})

	t.Run("unmapped context IDs are left out", func(t *testing.T) {
		st := newStore(t)
		commit(t, st, nil, store.ContextChange{ContextID: one, New: record(t)})
		got, err := st.LoadContexts(ctx, st.provider, [][]byte{one, absent})
		require.NoError(t, err)
		require.Equal(t, []string{"one"}, keys(got))
	})

	t.Run("a commit replaces an existing record", func(t *testing.T) {
		st := newStore(t)
		first := commit(t, st, nil, store.ContextChange{ContextID: one, New: record(t)})
		replaced := record(t)
		commit(t, st, first, store.ContextChange{ContextID: one, New: replaced})
		got, err := st.LoadContexts(ctx, st.provider, [][]byte{one})
		require.NoError(t, err)
		requireRecords(t, map[string]store.ContextRecord{"one": replaced}, got)
	})

	t.Run("an empty record removes the row", func(t *testing.T) {
		st := newStore(t)
		first := commit(t, st, nil, store.ContextChange{ContextID: one, New: record(t)}, store.ContextChange{ContextID: two, New: record(t)})
		commit(t, st, first, store.ContextChange{ContextID: one}, store.ContextChange{ContextID: two})
		require.Zero(t, countContexts(t, st.db))
	})

	t.Run("a failed head swap writes no record", func(t *testing.T) {
		st := newStore(t)
		commit(t, st, nil, store.ContextChange{ContextID: one, New: record(t)})
		err := st.CommitBatch(ctx, st.provider, []store.ContextChange{{ContextID: two, New: record(t)}}, nil, signedHead(t))
		require.ErrorIs(t, err, store.ErrPreconditionFailed)
		got, err := st.LoadContexts(ctx, st.provider, [][]byte{two})
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("another provider is refused", func(t *testing.T) {
		st := newStore(t)
		_, err := st.LoadContexts(ctx, peerOf(t, testutil.Bob), [][]byte{one})
		require.ErrorContains(t, err, "holds provider")
	})
}

func TestPutAdverts(t *testing.T) {
	ctx := t.Context()

	t.Run("writes every advert", func(t *testing.T) {
		st := newStore(t)
		advs := []schema.Advertisement{advert(t), advert(t)}
		require.NoError(t, st.PutAdverts(ctx, advs))
		for _, adv := range advs {
			lnk, err := store.PutAdvert(ctx, discard{}, adv)
			require.NoError(t, err)
			_, err = st.Advert(ctx, lnk)
			require.NoError(t, err, "advert %s", lnk)
		}
	})

	t.Run("a failed write fails the batch", func(t *testing.T) {
		db, provider := newDB(t)
		st := New(db, failingPuts{memory.NewStore()}, provider)
		err := st.PutAdverts(ctx, []schema.Advertisement{advert(t)})
		require.ErrorIs(t, err, errPutFailed)
	})
}

func newStore(t *testing.T) *Store {
	t.Helper()
	db, provider := newDB(t)
	return New(db, memory.NewStore(), provider)
}

// commit commits the changes on top of the given head and returns the new
// head.
func commit(t *testing.T, st *Store, oldHead *head.SignedHead, changes ...store.ContextChange) *head.SignedHead {
	t.Helper()
	newHead := signedHead(t)
	require.NoError(t, st.CommitBatch(t.Context(), st.provider, changes, oldHead, newHead))
	return newHead
}

func signedHead(t *testing.T) *head.SignedHead {
	t.Helper()
	key, err := lib.Libp2pKey(testutil.Alice)
	require.NoError(t, err)
	h, err := head.NewSignedHead(testutil.RandomCID(t), "/indexer/ingest/test", key)
	require.NoError(t, err)
	return h
}

func record(t *testing.T) store.ContextRecord {
	t.Helper()
	md := metadata.MetadataContext.New(&metadata.LocationCommitmentMetadata{Claim: testutil.RandomCID(t)})
	return store.ContextRecord{ChunkLink: cidlink.Link{Cid: testutil.RandomCID(t)}, Metadata: &md}
}

func advert(t *testing.T) schema.Advertisement {
	t.Helper()
	key, err := lib.Libp2pKey(testutil.Alice)
	require.NoError(t, err)
	meta := metadata.MetadataContext.New(&metadata.LocationCommitmentMetadata{Claim: testutil.RandomCID(t)})
	md, err := meta.MarshalBinary()
	require.NoError(t, err)
	adv := schema.Advertisement{
		Provider:  peerOf(t, testutil.Alice).String(),
		Entries:   cidlink.Link{Cid: testutil.RandomCID(t)},
		ContextID: []byte(testutil.RandomCID(t).String()),
		Metadata:  md,
	}
	require.NoError(t, adv.Sign(key))
	return adv
}

// requireRecords compares records by their encoded form: decoded metadata
// does not compare equal with require.Equal.
func requireRecords(t *testing.T, want, got map[string]store.ContextRecord) {
	t.Helper()
	require.Equal(t, encodeRecords(t, want), encodeRecords(t, got))
}

func encodeRecords(t *testing.T, records map[string]store.ContextRecord) map[string][2]string {
	t.Helper()
	encoded := map[string][2]string{}
	for id, r := range records {
		var e [2]string
		if r.ChunkLink != nil {
			e[0] = r.ChunkLink.String()
		}
		if r.Metadata != nil {
			b, err := r.Metadata.MarshalBinary()
			require.NoError(t, err)
			e[1] = string(b)
		}
		encoded[id] = e
	}
	return encoded
}

func keys(records map[string]store.ContextRecord) []string {
	var ids []string
	for id := range records {
		ids = append(ids, id)
	}
	return ids
}

// discard is a block store that keeps nothing, for computing a link.
type discard struct{}

func (discard) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("discard keeps no blocks")
}

func (discard) Put(context.Context, string, uint64, io.Reader) error { return nil }

var errPutFailed = errors.New("put failed")

type failingPuts struct {
	objectstore.Store
}

func (failingPuts) Put(context.Context, string, uint64, io.Reader) error {
	return errPutFailed
}
