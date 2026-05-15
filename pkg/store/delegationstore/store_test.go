package delegationstore

import (
	"testing"

	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/ipfs/go-datastore"
	"github.com/stretchr/testify/require"
)

func TestDelegationStore(t *testing.T) {
	t.Run("roundtrip", func(t *testing.T) {
		s := NewDatastoreStore(datastore.NewMapDatastore())

		issuer := testutil.RandomSigner(t)
		audience := testutil.RandomDID(t)

		dlg, err := delegation.Delegate(
			issuer,
			audience,
			issuer.DID(),
			"/test/test",
			delegation.WithNoExpiration(),
		)
		require.NoError(t, err)

		err = s.Put(t.Context(), dlg)
		require.NoError(t, err)

		got, err := s.Get(t.Context(), dlg.Link())
		require.NoError(t, err)
		require.Equal(t, dlg.Bytes(), got.Bytes())
	})
}
