package consolidationstore

import (
	"testing"

	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ipld/datamodel"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/ipfs/go-datastore"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/consolidationstore/consolidation"
)

func TestDatastoreConsolidationStore(t *testing.T) {
	t.Run("roundtrip", func(t *testing.T) {
		ds := datastore.NewMapDatastore()
		s := NewDatastoreStore(ds)

		batchCID := testutil.RandomCID(t)
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

		batchCID := testutil.RandomCID(t)

		_, err := s.Get(t.Context(), batchCID)
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("delete", func(t *testing.T) {
		ds := datastore.NewMapDatastore()
		s := NewDatastoreStore(ds)

		batchCID := testutil.RandomCID(t)
		c := createTestConsolidation(t)

		err := s.Put(t.Context(), batchCID, c)
		require.NoError(t, err)

		got, err := s.Get(t.Context(), batchCID)
		require.NoError(t, err)
		requireEqualConsolidation(t, c, got)

		err = s.Delete(t.Context(), batchCID)
		require.NoError(t, err)

		_, err = s.Get(t.Context(), batchCID)
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("delete non-existent", func(t *testing.T) {
		ds := datastore.NewMapDatastore()
		s := NewDatastoreStore(ds)

		err := s.Delete(t.Context(), testutil.RandomCID(t))
		require.NoError(t, err)
	})

	t.Run("overwrite", func(t *testing.T) {
		ds := datastore.NewMapDatastore()
		s := NewDatastoreStore(ds)

		batchCID := testutil.RandomCID(t)
		c1 := createTestConsolidation(t)
		c2 := createTestConsolidation(t)

		err := s.Put(t.Context(), batchCID, c1)
		require.NoError(t, err)
		err = s.Put(t.Context(), batchCID, c2)
		require.NoError(t, err)

		got, err := s.Get(t.Context(), batchCID)
		require.NoError(t, err)
		requireEqualConsolidation(t, c2, got)
	})

	// The UCAN 0.x → 1.0 migration removed DatastoreLegacyReader. The previous
	// "legacy migration" subtest exercised CAR-archived UCAN 0.x track
	// invocations being read and re-written; that path is dead. New
	// deployments only see the cborgen format produced by Codec{}.
}

func createTestConsolidation(t *testing.T) consolidation.Consolidation {
	t.Helper()

	signer := testutil.RandomSigner(t)
	audience := testutil.RandomDID(t)

	track, err := invocation.Invoke(
		signer,
		signer.DID(),
		"/space/egress/track",
		datamodel.Map{},
		invocation.WithAudience(audience),
	)
	require.NoError(t, err)

	return consolidation.New(track, testutil.RandomCID(t))
}

func requireEqualConsolidation(t *testing.T, expected, actual consolidation.Consolidation) {
	t.Helper()

	require.Equal(t, expected.TrackInvocationBytes, actual.TrackInvocationBytes)
	require.Equal(t, expected.ConsolidateInvocationCID, actual.ConsolidateInvocationCID)
}
