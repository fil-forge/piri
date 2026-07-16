package blob

import (
	"testing"

	"github.com/fil-forge/libforge/commands"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan/command"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/internal/testutil/pdpfake"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
)

// removeWorld bundles the stores backing RemoveDeps so tests can seed and
// inspect state.
type removeWorld struct {
	deps    RemoveDeps
	allocs  *allocationstore.Store
	accepts *acceptancestore.Store
	claims  *invocationstore.Store
	pieces  *pdpfake.Pieces
}

func newRemoveWorld(t *testing.T) *removeWorld {
	t.Helper()
	// Separate backends per store — production namespaces them; a shared
	// flat datastore would leak allocation rows into acceptance scans.
	w := &removeWorld{
		allocs:  allocationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore())),
		accepts: acceptancestore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore())),
		claims:  invocationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore())),
		pieces:  pdpfake.NewPieces(),
	}
	w.deps = RemoveDeps{
		Allocations: w.allocs,
		Acceptances: w.accepts,
		ClaimStore:  w.claims,
		Pieces:      w.pieces,
	}
	return w
}

func TestRemove_UnknownBlobIsSuccess(t *testing.T) {
	w := newRemoveWorld(t)

	err := Remove(t.Context(), w.deps, &RemoveRequest{
		Space:  testutil.RandomDID(t),
		Digest: testutil.RandomMultihash(t),
	})
	require.NoError(t, err, "removing a blob that was never stored is idempotent success")
	// Nothing claimed the digest, so removal of the (nonexistent) bytes is requested.
	require.Len(t, w.pieces.Removed(), 1)
}

func TestRemove_ReleasesClaimAndBytes(t *testing.T) {
	w := newRemoveWorld(t)
	digest := testutil.RandomMultihash(t)
	space := testutil.RandomDID(t)

	claim, err := invocation.Invoke(
		testutil.RandomIssuer(t),
		testutil.RandomDID(t),
		command.New("/assert/location"),
		&commands.Unit{},
	)
	require.NoError(t, err)
	claimLink := claim.Link()
	require.NoError(t, w.claims.Put(t.Context(), claim))
	require.NoError(t, w.allocs.Put(t.Context(), allocation.Allocation{
		Space: space,
		Blob:  blob.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}))
	require.NoError(t, w.accepts.Put(t.Context(), acceptance.Acceptance{
		Space: space,
		Blob:  acceptance.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
		Claim: &claimLink,
	}))
	w.pieces.Put(digest, []byte("data"))

	require.NoError(t, Remove(t.Context(), w.deps, &RemoveRequest{Space: space, Digest: digest}))

	_, err = w.allocs.Get(t.Context(), digest, space)
	require.ErrorIs(t, err, store.ErrNotFound, "allocation deleted")
	_, err = w.accepts.Get(t.Context(), digest, space)
	require.ErrorIs(t, err, store.ErrNotFound, "acceptance deleted")
	_, err = w.claims.Get(t.Context(), claimLink)
	require.ErrorIs(t, err, store.ErrNotFound, "location claim deleted")
	require.Len(t, w.pieces.Removed(), 1, "no space claims the digest — bytes released")

	// Removing again is idempotent success.
	require.NoError(t, Remove(t.Context(), w.deps, &RemoveRequest{Space: space, Digest: digest}))
}

func TestRemove_OtherSpaceClaimRetainsBytes(t *testing.T) {
	w := newRemoveWorld(t)
	digest := testutil.RandomMultihash(t)
	removingSpace := testutil.RandomDID(t)
	otherSpace := testutil.RandomDID(t)

	require.NoError(t, w.accepts.Put(t.Context(), acceptance.Acceptance{
		Space: removingSpace,
		Blob:  acceptance.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}))
	require.NoError(t, w.accepts.Put(t.Context(), acceptance.Acceptance{
		Space: otherSpace,
		Blob:  acceptance.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}))
	w.pieces.Put(digest, []byte("data"))

	require.NoError(t, Remove(t.Context(), w.deps, &RemoveRequest{Space: removingSpace, Digest: digest}))

	_, err := w.accepts.Get(t.Context(), digest, removingSpace)
	require.ErrorIs(t, err, store.ErrNotFound, "removing space's acceptance deleted")
	_, err = w.accepts.Get(t.Context(), digest, otherSpace)
	require.NoError(t, err, "other space's acceptance retained")
	require.Empty(t, w.pieces.Removed(), "other space still claims the digest — bytes retained")

	// Releasing the last claim releases the bytes.
	require.NoError(t, Remove(t.Context(), w.deps, &RemoveRequest{Space: otherSpace, Digest: digest}))
	require.Len(t, w.pieces.Removed(), 1)
}

func TestRemove_LiveAllocationRetainsBytes(t *testing.T) {
	// An allocation from another space (upload possibly in flight) blocks
	// physical deletion even when no acceptance exists.
	w := newRemoveWorld(t)
	digest := testutil.RandomMultihash(t)
	removingSpace := testutil.RandomDID(t)
	uploadingSpace := testutil.RandomDID(t)

	require.NoError(t, w.accepts.Put(t.Context(), acceptance.Acceptance{
		Space: removingSpace,
		Blob:  acceptance.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}))
	require.NoError(t, w.allocs.Put(t.Context(), allocation.Allocation{
		Space: uploadingSpace,
		Blob:  blob.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}))

	require.NoError(t, Remove(t.Context(), w.deps, &RemoveRequest{Space: removingSpace, Digest: digest}))
	require.Empty(t, w.pieces.Removed(), "live allocation in another space — bytes retained")
}

func TestRemove_AcceptanceWithoutClaimLink(t *testing.T) {
	// Records written before the Claim field existed have no claim link;
	// removal must still succeed.
	w := newRemoveWorld(t)
	digest := testutil.RandomMultihash(t)
	space := testutil.RandomDID(t)

	require.NoError(t, w.accepts.Put(t.Context(), acceptance.Acceptance{
		Space: space,
		Blob:  acceptance.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}))

	require.NoError(t, Remove(t.Context(), w.deps, &RemoveRequest{Space: space, Digest: digest}))
	_, err := w.accepts.Get(t.Context(), digest, space)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Len(t, w.pieces.Removed(), 1)
}
