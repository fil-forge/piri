package invocationstore

import (
	"testing"

	"github.com/fil-forge/libforge/commands"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan/command"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/stretchr/testify/require"
)

func testInvocationStore(t *testing.T, newStore func(*testing.T) InvocationStore) {
	t.Run("roundtrip", func(t *testing.T) {
		store := newStore(t)

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

func TestInvocationStore(t *testing.T) { forEachImpl(t, testInvocationStore) }
