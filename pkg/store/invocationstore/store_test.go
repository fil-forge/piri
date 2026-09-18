package invocationstore

import (
	"github.com/ipfs/go-cid"
	"testing"

	"github.com/fil-forge/libforge/commands"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan/command"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/ipfs/go-datastore"
	"github.com/stretchr/testify/require"
)

func TestInvocationStore(t *testing.T) {
	t.Run("roundtrip", func(t *testing.T) {
		store := NewDatastoreStore(datastore.NewMapDatastore())

		inv, err := invocation.Invoke(
			testutil.RandomIssuer(t),
			testutil.RandomDID(t),
			command.New("/whatever"),
			&commands.Unit{},
		)
		require.NoError(t, err)

		err = store.Put(t.Context(), inv)
		require.NoError(t, err)

		res, err := store.Get(t.Context(), inv.Link())
		require.NoError(t, err)
		require.Equal(t, inv.Link(), res.Link())
	})
}

// TestInvocationStore_GetAll pins the batch read: every stored link comes back
// keyed by CID, a link the store lacks is simply absent, and an empty request
// is an empty result.
func TestInvocationStore_GetAll(t *testing.T) {
	ctx := t.Context()
	s := NewDatastoreStore(datastore.NewMapDatastore())

	var links []cid.Cid
	for range 3 {
		inv, err := invocation.Invoke(testutil.RandomIssuer(t), testutil.RandomDID(t), command.New("/test/thing"), &commands.Unit{})
		require.NoError(t, err)
		require.NoError(t, s.Put(ctx, inv))
		links = append(links, inv.Link())
	}
	missing := testutil.RandomCID(t)

	got, err := s.GetAll(ctx, append(links, missing))
	require.NoError(t, err)
	require.Len(t, got, 3)
	for _, l := range links {
		require.NotNil(t, got[l], "stored invocation %s missing from GetAll", l)
		require.Equal(t, l, got[l].Link())
	}
	require.NotContains(t, got, missing)

	empty, err := s.GetAll(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}
