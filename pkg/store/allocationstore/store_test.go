package allocationstore

import (
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/stretchr/testify/assert"
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
func TestGetByTask(t *testing.T) {
	ctx := t.Context()
	s := NewDatastoreStore(datastore.NewMapDatastore())
	space := testutil.RandomDID(t)
	b := blob.Blob{Digest: testutil.RandomMultihash(t), Size: 1}
	first := allocation.Allocation{Space: space, Blob: b, Cause: testutil.RandomCID(t), Allocation: testutil.RandomCID(t)}
	require.NoError(t, s.Put(ctx, first))

	got, err := s.GetByTask(ctx, first.Allocation)
	require.NoError(t, err)
	require.Equal(t, first, got)

	second := first
	second.Cause, second.Allocation = testutil.RandomCID(t), testutil.RandomCID(t)
	require.NoError(t, s.Put(ctx, second))
	_, err = s.GetByTask(ctx, first.Allocation)
	require.ErrorIs(t, err, store.ErrNotFound, "a replaced allocation is not found")
	got, err = s.GetByTask(ctx, second.Allocation)
	require.NoError(t, err)
	require.Equal(t, second, got)

	require.NoError(t, s.Delete(ctx, b.Digest, space))
	_, err = s.GetByTask(ctx, second.Allocation)
	require.ErrorIs(t, err, store.ErrNotFound, "a deleted allocation is not found")
	require.NoError(t, s.Delete(ctx, b.Digest, space), "deleting again succeeds")

	_, err = s.GetByTask(ctx, testutil.RandomCID(t))
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestConditionalChanges(t *testing.T) {
	ctx := t.Context()
	s := NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
	space := testutil.RandomDID(t)
	b := blob.Blob{Digest: testutil.RandomMultihash(t), Size: 1}
	first := allocation.Allocation{Space: space, Blob: b, Cause: testutil.RandomCID(t), Allocation: testutil.RandomCID(t)}
	second := first
	second.Cause, second.Allocation = testutil.RandomCID(t), testutil.RandomCID(t)

	claimed, err := s.Claim(ctx, first)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = s.Claim(ctx, second)
	require.NoError(t, err)
	require.False(t, claimed, "a held claim is not taken")

	require.NoError(t, s.Put(ctx, second))
	deleted, err := s.ReleaseByTask(ctx, first.Allocation)
	require.NoError(t, err)
	require.False(t, deleted, "a replaced allocation's delete leaves the later one")
	got, err := s.Get(ctx, b.Digest, space)
	require.NoError(t, err)
	require.Equal(t, second.Allocation, got.Allocation)

	require.NoError(t, s.MakeCurrent(ctx, b.Digest, space, first.Allocation))
	got, err = s.GetByTask(ctx, first.Allocation)
	require.NoError(t, err, "the allocation made current is found by its link")
	require.Equal(t, first.Allocation, got.Allocation)
	_, err = s.GetByTask(ctx, second.Allocation)
	require.ErrorIs(t, err, store.ErrNotFound)

	deleted, err = s.ReleaseByTask(ctx, first.Allocation)
	require.NoError(t, err)
	require.True(t, deleted)
	_, err = s.Get(ctx, b.Digest, space)
	require.ErrorIs(t, err, store.ErrNotFound)

	require.NoError(t, s.MakeCurrent(ctx, b.Digest, space, first.Allocation), "a space with no allocation is left alone")
	_, err = s.Get(ctx, b.Digest, space)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// Two received uploads of the same content in a space share one claim.
// Releasing both at once leaves neither the claim nor either record: a release
// sees the other's handover, never a claim handed to a record already gone.
func TestReleasePendingConcurrently(t *testing.T) {
	ctx := t.Context()
	for range 50 {
		s := NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
		space := testutil.RandomDID(t)
		digest := testutil.RandomMultihash(t)
		pending := func() allocation.Pending {
			p := allocation.Pending{
				Allocation: testutil.RandomCID(t),
				Space:      space,
				Size:       1,
				DigestCode: 0x12,
				Cause:      testutil.RandomCID(t),
				UploadID:   "upload",
				Digest:     digest,
			}
			require.NoError(t, s.PutPending(ctx, p))
			return p
		}
		a, b := pending(), pending()
		_, err := s.Claim(ctx, allocation.Allocation{
			Space: space, Blob: blob.Blob{Digest: digest, Size: 1}, Cause: a.Cause, Allocation: a.Allocation,
		})
		require.NoError(t, err)

		var wg sync.WaitGroup
		for _, p := range []allocation.Pending{a, b} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.ReleasePending(ctx, p)
				assert.NoError(t, err)
			}()
		}
		wg.Wait()

		_, err = s.Get(ctx, digest, space)
		require.ErrorIs(t, err, store.ErrNotFound, "no claim is left for released uploads")
		for _, p := range []allocation.Pending{a, b} {
			_, err := s.GetPending(ctx, p.Allocation)
			require.ErrorIs(t, err, store.ErrNotFound)
		}
	}
}

// Releasing an allocation hands its claim to another received upload of the
// same content in the space, rather than deleting it under that upload.
func TestReleaseByTaskHandsOver(t *testing.T) {
	ctx := t.Context()
	s := NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
	space := testutil.RandomDID(t)
	b := blob.Blob{Digest: testutil.RandomMultihash(t), Size: 1}
	held := allocation.Allocation{Space: space, Blob: b, Cause: testutil.RandomCID(t), Allocation: testutil.RandomCID(t)}
	require.NoError(t, s.Put(ctx, held))
	waiting := allocation.Pending{
		Allocation: testutil.RandomCID(t),
		Space:      space,
		Size:       1,
		DigestCode: 0x12,
		Cause:      testutil.RandomCID(t),
		UploadID:   "upload",
		Digest:     b.Digest,
	}
	require.NoError(t, s.PutPending(ctx, waiting))

	released, err := s.ReleaseByTask(ctx, held.Allocation)
	require.NoError(t, err)
	require.True(t, released)
	got, err := s.Get(ctx, b.Digest, space)
	require.NoError(t, err, "the claim is handed over, not deleted")
	require.Equal(t, waiting.Allocation, got.Allocation)
	_, err = s.GetByTask(ctx, waiting.Allocation)
	require.NoError(t, err)

	released, err = s.ReleaseByTask(ctx, held.Allocation)
	require.NoError(t, err)
	require.False(t, released, "a released allocation is not released again")
}
