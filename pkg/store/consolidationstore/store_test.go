package consolidationstore

import (
	"testing"

	"github.com/fil-forge/libforge/commands"
	"github.com/fil-forge/libforge/commands/space/egress"
	"github.com/fil-forge/ucantone/testutil"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/consolidationstore/consolidation"
)

func TestDatastoreConsolidationStore(t *testing.T) {
	t.Run("roundtrip", func(t *testing.T) {
		ds := datastore.NewMapDatastore()
		s := NewDatastoreStore(ds)

		batchCID := randomCID(t)
		c := createTestConsolidation(t)

		err := s.Put(t.Context(), batchCID, c)
		require.NoError(t, err)

		got, err := s.Get(t.Context(), batchCID)
		require.NoError(t, err)
		requireEqualConsolidation(t, c, got)
	})

	t.Run("not found", func(t *testing.T) {
		ds := datastore.NewMapDatastore()
		s := NewDatastoreStore(ds)

		batchCID := randomCID(t)

		_, err := s.Get(t.Context(), batchCID)
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("delete", func(t *testing.T) {
		ds := datastore.NewMapDatastore()
		s := NewDatastoreStore(ds)

		batchCID := randomCID(t)
		c := createTestConsolidation(t)

		// Put
		err := s.Put(t.Context(), batchCID, c)
		require.NoError(t, err)

		// Verify exists
		got, err := s.Get(t.Context(), batchCID)
		require.NoError(t, err)
		requireEqualConsolidation(t, c, got)

		// Delete
		err = s.Delete(t.Context(), batchCID)
		require.NoError(t, err)

		// Verify not found
		_, err = s.Get(t.Context(), batchCID)
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("delete non-existent", func(t *testing.T) {
		ds := datastore.NewMapDatastore()
		s := NewDatastoreStore(ds)

		batchCID := randomCID(t)

		// Delete should not error on non-existent key
		err := s.Delete(t.Context(), batchCID)
		require.NoError(t, err)
	})

	t.Run("overwrite", func(t *testing.T) {
		ds := datastore.NewMapDatastore()
		s := NewDatastoreStore(ds)

		batchCID := randomCID(t)
		c1 := createTestConsolidation(t)
		c2 := createTestConsolidation(t)

		// Put first
		err := s.Put(t.Context(), batchCID, c1)
		require.NoError(t, err)

		// Overwrite with second
		err = s.Put(t.Context(), batchCID, c2)
		require.NoError(t, err)

		// Get should return second
		got, err := s.Get(t.Context(), batchCID)
		require.NoError(t, err)
		requireEqualConsolidation(t, c2, got)
	})
}

func createTestConsolidation(t *testing.T) consolidation.Consolidation {
	t.Helper()

	issuer := testutil.RandomIssuer(t)
	audience := testutil.RandomDID(t)

	inv, err := egress.Track.Invoke(
		issuer,
		audience,
		&egress.TrackArguments{
			Receipts: testutil.RandomCID(t),
			Endpoint: commands.CborURL{},
		},
	)
	require.NoError(t, err)

	return consolidation.New(inv, randomCID(t))
}

func randomCID(t *testing.T) cid.Cid {
	t.Helper()
	return testutil.RandomCID(t)
}

func requireEqualConsolidation(t *testing.T, expected, actual consolidation.Consolidation) {
	t.Helper()

	// Persisted bytes round-trip exactly.
	require.Equal(t, expected.TrackInvocationBytes, actual.TrackInvocationBytes)
	require.Equal(t, expected.ConsolidateInvocationCID, actual.ConsolidateInvocationCID)
}
