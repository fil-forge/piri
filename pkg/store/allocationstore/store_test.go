package allocationstore

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/ipfs/go-datastore"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

func TestDatastoreAllocationStore(t *testing.T) {
	t.Run("roundtrip", func(t *testing.T) {
		s := NewDatastoreStore(datastore.NewMapDatastore())

		alloc := allocation.Allocation{
			Allocation: testutil.RandomCID(t),
			Space:      testutil.RandomDID(t),
			Blob: blob.Blob{
				Digest: testutil.RandomMultihash(t),
				Size:   uint64(1 + rand.IntN(1000)),
			},
			Expires: ucan.UnixTimestamp(time.Now().Unix()),
			Cause:   testutil.RandomCID(t),
		}

		err := s.Put(t.Context(), alloc)
		require.NoError(t, err)

		got, err := s.Get(t.Context(), alloc.Blob.Digest, alloc.Space)
		require.NoError(t, err)
		require.Equal(t, alloc, got)
	})

	t.Run("get any", func(t *testing.T) {
		s := NewDatastoreStore(datastore.NewMapDatastore())

		alloc := allocation.Allocation{
			Allocation: testutil.RandomCID(t),
			Space:      testutil.RandomDID(t),
			Blob: blob.Blob{
				Digest: testutil.RandomMultihash(t),
				Size:   uint64(1 + rand.IntN(1000)),
			},
			Expires: ucan.UnixTimestamp(time.Now().Unix()),
			Cause:   testutil.RandomCID(t),
		}

		err := s.Put(t.Context(), alloc)
		require.NoError(t, err)

		got, err := s.GetAny(t.Context(), alloc.Blob.Digest)
		require.NoError(t, err)
		require.Equal(t, alloc, got)
	})

	t.Run("exists", func(t *testing.T) {
		s := NewDatastoreStore(datastore.NewMapDatastore())

		alloc := allocation.Allocation{
			Allocation: testutil.RandomCID(t),
			Space:      testutil.RandomDID(t),
			Blob: blob.Blob{
				Digest: testutil.RandomMultihash(t),
				Size:   uint64(1 + rand.IntN(1000)),
			},
			Expires: ucan.UnixTimestamp(time.Now().Unix()),
			Cause:   testutil.RandomCID(t),
		}

		exists, err := s.Exists(t.Context(), alloc.Blob.Digest)
		require.NoError(t, err)
		require.False(t, exists)

		err = s.Put(t.Context(), alloc)
		require.NoError(t, err)

		exists, err = s.Exists(t.Context(), alloc.Blob.Digest)
		require.NoError(t, err)
		require.True(t, exists)
	})

	t.Run("multiple spaces same blob", func(t *testing.T) {
		s := NewDatastoreStore(datastore.NewMapDatastore())

		blb := blob.Blob{
			Digest: testutil.RandomMultihash(t),
			Size:   uint64(1 + rand.IntN(1000)),
		}

		alloc0 := allocation.Allocation{
			Allocation: testutil.RandomCID(t),
			Space:      testutil.RandomDID(t),
			Blob:       blb,
			Expires:    ucan.UnixTimestamp(time.Now().Unix()),
			Cause:      testutil.RandomCID(t),
		}

		alloc1 := allocation.Allocation{
			Allocation: testutil.RandomCID(t),
			Space:      testutil.RandomDID(t),
			Blob:       blb,
			Expires:    ucan.UnixTimestamp(time.Now().Unix()),
			Cause:      testutil.RandomCID(t),
		}

		err := s.Put(t.Context(), alloc0)
		require.NoError(t, err)
		err = s.Put(t.Context(), alloc1)
		require.NoError(t, err)

		// Get specific allocations
		got0, err := s.Get(t.Context(), blb.Digest, alloc0.Space)
		require.NoError(t, err)
		require.Equal(t, alloc0, got0)

		got1, err := s.Get(t.Context(), blb.Digest, alloc1.Space)
		require.NoError(t, err)
		require.Equal(t, alloc1, got1)

		// GetAny returns one of them
		gotAny, err := s.GetAny(t.Context(), blb.Digest)
		require.NoError(t, err)
		require.True(t, gotAny.Space == alloc0.Space || gotAny.Space == alloc1.Space)

		// Exists returns true
		exists, err := s.Exists(t.Context(), blb.Digest)
		require.NoError(t, err)
		require.True(t, exists)
	})

	t.Run("not found", func(t *testing.T) {
		s := NewDatastoreStore(datastore.NewMapDatastore())

		digest := testutil.RandomMultihash(t)
		space := testutil.RandomDID(t)

		_, err := s.Get(t.Context(), digest, space)
		require.ErrorIs(t, err, store.ErrNotFound)

		_, err = s.GetAny(t.Context(), digest)
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("get any non-expired with mixed allocations", func(t *testing.T) {
		s := NewDatastoreStore(datastore.NewMapDatastore())

		blb := blob.Blob{
			Digest: testutil.RandomMultihash(t),
			Size:   uint64(1 + rand.IntN(1000)),
		}

		now := ucan.Now()
		// Expired allocation
		expiredAlloc := allocation.Allocation{
			Allocation: testutil.RandomCID(t),
			Space:      testutil.RandomDID(t),
			Blob:       blb,
			Expires:    now - 100, // expired 100 seconds ago
			Cause:      testutil.RandomCID(t),
		}

		// Valid allocation
		validAlloc := allocation.Allocation{
			Allocation: testutil.RandomCID(t),
			Space:      testutil.RandomDID(t),
			Blob:       blb,
			Expires:    now + 3600, // expires in 1 hour
			Cause:      testutil.RandomCID(t),
		}

		// Put expired first
		err := s.Put(t.Context(), expiredAlloc)
		require.NoError(t, err)
		err = s.Put(t.Context(), validAlloc)
		require.NoError(t, err)

		// GetAnyNonExpired should return the valid one
		got, err := s.GetAnyNonExpired(t.Context(), blb.Digest, now)
		require.NoError(t, err)
		require.Equal(t, validAlloc, got)
	})

	t.Run("get any non-expired all expired", func(t *testing.T) {
		s := NewDatastoreStore(datastore.NewMapDatastore())

		blb := blob.Blob{
			Digest: testutil.RandomMultihash(t),
			Size:   uint64(1 + rand.IntN(1000)),
		}

		now := ucan.Now()

		expiredAlloc := allocation.Allocation{
			Allocation: testutil.RandomCID(t),
			Space:      testutil.RandomDID(t),
			Blob:       blb,
			Expires:    now - 100,
			Cause:      testutil.RandomCID(t),
		}

		err := s.Put(t.Context(), expiredAlloc)
		require.NoError(t, err)

		_, err = s.GetAnyNonExpired(t.Context(), blb.Digest, now)
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("get any non-expired not found", func(t *testing.T) {
		s := NewDatastoreStore(datastore.NewMapDatastore())

		digest := testutil.RandomMultihash(t)
		now := ucan.Now()

		_, err := s.GetAnyNonExpired(t.Context(), digest, now)
		require.ErrorIs(t, err, store.ErrNotFound)
	})
}

// An allocation is found by its allocate task while it is the space's
// allocation for its blob, and not once a later allocation replaces it or it
// is deleted.
func TestGetByAllocation(t *testing.T) {
	ctx := t.Context()
	s := NewDatastoreStore(datastore.NewMapDatastore())
	space := testutil.RandomDID(t)
	b := blob.Blob{Digest: testutil.RandomMultihash(t), Size: 1}
	first := allocation.Allocation{Space: space, Blob: b, Cause: testutil.RandomCID(t), Allocation: testutil.RandomCID(t)}
	require.NoError(t, s.Put(ctx, first))

	got, err := s.GetByAllocation(ctx, first.Allocation)
	require.NoError(t, err)
	require.Equal(t, first, got)

	second := first
	second.Cause, second.Allocation = testutil.RandomCID(t), testutil.RandomCID(t)
	require.NoError(t, s.Put(ctx, second))
	_, err = s.GetByAllocation(ctx, first.Allocation)
	require.ErrorIs(t, err, store.ErrNotFound, "a replaced allocation is not found")
	got, err = s.GetByAllocation(ctx, second.Allocation)
	require.NoError(t, err)
	require.Equal(t, second, got)

	require.NoError(t, s.Delete(ctx, b.Digest, space))
	_, err = s.GetByAllocation(ctx, second.Allocation)
	require.ErrorIs(t, err, store.ErrNotFound, "a deleted allocation is not found")
	require.NoError(t, s.Delete(ctx, b.Digest, space), "deleting again succeeds")

	_, err = s.GetByAllocation(ctx, testutil.RandomCID(t))
	require.ErrorIs(t, err, store.ErrNotFound)
}
