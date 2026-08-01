package blob

import (
	"testing"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/internal/testutil/pdpfake"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

// newAllocateDeps returns Deps backed by in-memory implementations suitable
// for unit testing. Tests can preload state via the returned values.
func newAllocateDeps(t *testing.T) (AllocateDeps, *pdpfake.Pieces, *allocationstore.Store) {
	t.Helper()
	ds := dssync.MutexWrap(datastore.NewMapDatastore())
	allocs := allocationstore.NewDatastoreStore(ds)
	pieces := pdpfake.NewPieces()
	return AllocateDeps{
		Allocations: allocs,
		Pieces:      pieces,
	}, pieces, allocs
}

func TestAllocate_NoExistingAllocationOrBlob(t *testing.T) {
	deps, _, allocs := newAllocateDeps(t)

	digest := testutil.RandomMultihash(t)
	space := testutil.RandomDID(t)
	cause := testutil.RandomCID(t)

	resp, err := Allocate(t.Context(), deps, &AllocateRequest{
		Space: space,
		Blob:  blob.Blob{Digest: digest, Size: 256},
		Cause: cause,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(256), resp.Size, "size reflects the full requested allocation")
	require.NotNil(t, resp.Address, "address present so the caller can upload")

	stored, err := allocs.Get(t.Context(), digest, space)
	require.NoError(t, err)
	require.Equal(t, digest, stored.Blob.Digest)
	require.Equal(t, space, stored.Space)
	require.Equal(t, cause, stored.Cause)
}

func TestAllocate_ExistingAllocationButBlobNotReceived(t *testing.T) {
	deps, _, allocs := newAllocateDeps(t)
	digest := testutil.RandomMultihash(t)
	space := testutil.RandomDID(t)

	require.NoError(t, allocs.Put(t.Context(), allocation.Allocation{
		Space: space,
		Blob:  blob.Blob{Digest: digest, Size: 256},
		Cause: testutil.RandomCID(t),
	}))

	resp, err := Allocate(t.Context(), deps, &AllocateRequest{
		Space: space,
		Blob:  blob.Blob{Digest: digest, Size: 256},
		Cause: testutil.RandomCID(t),
	})
	require.NoError(t, err)
	require.Equal(t, uint64(0), resp.Size, "allocation already existed, no new space allocated")
	require.NotNil(t, resp.Address, "still need an upload URL — blob not yet received")
}

func TestAllocate_BlobAlreadyReceivedInSameSpace(t *testing.T) {
	deps, pieces, allocs := newAllocateDeps(t)
	digest := testutil.RandomMultihash(t)
	space := testutil.RandomDID(t)

	require.NoError(t, allocs.Put(t.Context(), allocation.Allocation{
		Space: space,
		Blob:  blob.Blob{Digest: digest, Size: 256},
		Cause: testutil.RandomCID(t),
	}))
	pieces.Put(digest, []byte("data"))

	resp, err := Allocate(t.Context(), deps, &AllocateRequest{
		Space: space,
		Blob:  blob.Blob{Digest: digest, Size: 256},
		Cause: testutil.RandomCID(t),
	})
	require.NoError(t, err)
	require.Equal(t, uint64(0), resp.Size)
	require.Nil(t, resp.Address, "blob already received — no upload URL needed")
}

func TestAllocate_BlobInOtherSpace(t *testing.T) {
	// Blob exists (stored from a previous space's allocation) but no allocation
	// in *this* space yet. We need a new allocation (size = blob size) but no
	// upload URL.
	deps, pieces, allocs := newAllocateDeps(t)
	digest := testutil.RandomMultihash(t)
	otherSpace := testutil.RandomDID(t)
	thisSpace := testutil.RandomDID(t)

	require.NoError(t, allocs.Put(t.Context(), allocation.Allocation{
		Space: otherSpace,
		Blob:  blob.Blob{Digest: digest, Size: 256},
		Cause: testutil.RandomCID(t),
	}))
	pieces.Put(digest, []byte("data"))

	resp, err := Allocate(t.Context(), deps, &AllocateRequest{
		Space: thisSpace,
		Blob:  blob.Blob{Digest: digest, Size: 256},
		Cause: testutil.RandomCID(t),
	})
	require.NoError(t, err)
	require.Equal(t, uint64(256), resp.Size, "first allocation in this space")
	require.Nil(t, resp.Address, "blob already on disk — no upload URL")
}

func TestAllocate_BlobPendingRemovalNoAllocations(t *testing.T) {
	// Regression: the bytes are still in the piece store but NO space holds
	// an allocation — the shape /blob/release leaves behind while the
	// pending-removal sweep hasn't run (delete → immediate re-add of the
	// same content). AllocatePiece finds the piece (Allocated=false, no
	// upload ID); the receipt must carry no address at all — an address
	// wrapping an empty URL sends the client a PUT to "". The allocation
	// row written here is a claim, so the sweep cancels the queued removal.
	deps, pieces, allocs := newAllocateDeps(t)
	digest := testutil.RandomMultihash(t)
	space := testutil.RandomDID(t)

	pieces.Put(digest, []byte("data")) // bytes present, zero allocations

	resp, err := Allocate(t.Context(), deps, &AllocateRequest{
		Space: space,
		Blob:  blob.Blob{Digest: digest, Size: 256},
		Cause: testutil.RandomCID(t),
	})
	require.NoError(t, err)
	require.Equal(t, uint64(256), resp.Size, "first allocation in this space")
	require.Nil(t, resp.Address, "bytes already on disk — no upload URL, and never an empty one")

	stored, err := allocs.Get(t.Context(), digest, space)
	require.NoError(t, err)
	require.Equal(t, space, stored.Space, "allocation recorded so the removal sweep sees a claim")
}

func TestAllocate_UnsupportedHashRejected(t *testing.T) {
	deps, _, _ := newAllocateDeps(t)
	// build a multihash with a function code not in HasherRegistry (e.g. blake2b-256)
	digest, err := multihash.EncodeName([]byte("anything"), "blake2b-256")
	require.NoError(t, err)

	_, err = Allocate(t.Context(), deps, &AllocateRequest{
		Space: testutil.RandomDID(t),
		Blob:  blob.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported hash")
}
