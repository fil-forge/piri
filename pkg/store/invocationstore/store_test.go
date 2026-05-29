package invocationstore

import (
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
			testutil.RandomSigner(t),
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
