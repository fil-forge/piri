package delegationstore

import (
	"testing"

	"github.com/fil-forge/go-libstoracha/testutil"
	"github.com/fil-forge/go-ucanto/core/result/ok"
	"github.com/fil-forge/go-ucanto/ucan"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/ipfs/go-datastore"
	"github.com/stretchr/testify/require"
)

func TestDelegationStore(t *testing.T) {
	t.Run("roundtrip", func(t *testing.T) {
		store := NewDatastoreStore(datastore.NewMapDatastore())

		dlg, err := delegation.Delegate(
			testutil.RandomSigner(t),
			testutil.RandomDID(t),
			[]ucan.Capability[ok.Unit]{
				ucan.NewCapability("test/test", testutil.RandomDID(t).String(), ok.Unit{}),
			},
		)
		require.NoError(t, err)

		err = store.Put(t.Context(), dlg)
		require.NoError(t, err)

		res, err := store.Get(t.Context(), dlg.Link())
		require.NoError(t, err)
		testutil.RequireEqualDelegation(t, dlg, res)
	})
}
