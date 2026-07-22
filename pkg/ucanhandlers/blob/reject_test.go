package blob

import (
	"testing"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	ucanerrors "github.com/fil-forge/ucantone/errors"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/internal/testutil/pdpfake"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

type rejectWorld struct {
	deps    RejectDeps
	allocs  *allocationstore.Store
	accepts *acceptancestore.Store
	pieces  *pdpfake.Pieces
}

func newRejectWorld(t *testing.T) *rejectWorld {
	t.Helper()
	// Separate backends per store — production namespaces them; a shared
	// flat datastore would leak allocation rows into acceptance scans.
	w := &rejectWorld{
		allocs:  allocationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore())),
		accepts: acceptancestore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore())),
		pieces:  pdpfake.NewPieces(),
	}
	w.deps = RejectDeps{
		Allocations: w.allocs,
		Acceptances: w.accepts,
		Pieces:      w.pieces,
	}
	return w
}

func TestReject_ParkedBlobDeletesBytes(t *testing.T) {
	w := newRejectWorld(t)
	digest := testutil.RandomMultihash(t)
	space := testutil.RandomDID(t)

	require.NoError(t, w.allocs.Put(t.Context(), allocation.Allocation{
		Space: space,
		Blob:  blob.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}))
	w.pieces.Put(digest, []byte("data"))

	require.NoError(t, Reject(t.Context(), w.deps, &RejectRequest{Space: space, Digest: digest}))

	_, err := w.allocs.Get(t.Context(), digest, space)
	require.ErrorIs(t, err, store.ErrNotFound, "allocation deleted")
	require.Len(t, w.pieces.Removed(), 1, "sole allocation gone — bytes released")

	// Idempotent.
	require.NoError(t, Reject(t.Context(), w.deps, &RejectRequest{Space: space, Digest: digest}))
}

func TestReject_UnknownBlobIsSuccess(t *testing.T) {
	w := newRejectWorld(t)
	require.NoError(t, Reject(t.Context(), w.deps, &RejectRequest{
		Space:  testutil.RandomDID(t),
		Digest: testutil.RandomMultihash(t),
	}))
}

func TestReject_AcceptedBlobRefused(t *testing.T) {
	w := newRejectWorld(t)
	digest := testutil.RandomMultihash(t)
	space := testutil.RandomDID(t)

	require.NoError(t, w.allocs.Put(t.Context(), allocation.Allocation{
		Space: space,
		Blob:  blob.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}))
	require.NoError(t, w.accepts.Put(t.Context(), acceptance.Acceptance{
		Space: space,
		Blob:  acceptance.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}))
	w.pieces.Put(digest, []byte("data"))

	err := Reject(t.Context(), w.deps, &RejectRequest{Space: space, Digest: digest})
	require.Error(t, err)
	var named ucanerrors.Named
	require.ErrorAs(t, err, &named)
	require.Equal(t, blob.BlobAcceptedErrorName, named.Name())

	_, err = w.allocs.Get(t.Context(), digest, space)
	require.NoError(t, err, "allocation untouched on refusal")
	require.Empty(t, w.pieces.Removed(), "accepted bytes never touched")
}

// The BlobAccepted guard is scoped to the invoking space, not the digest:
// another space accepting the same content-addressed bytes must never
// strand this space's parked allocation (RFC: multi-tenant liveness).
func TestReject_OtherSpaceAcceptanceDoesNotBlock(t *testing.T) {
	w := newRejectWorld(t)
	digest := testutil.RandomMultihash(t)
	rejecting := testutil.RandomDID(t)
	accepted := testutil.RandomDID(t)

	require.NoError(t, w.allocs.Put(t.Context(), allocation.Allocation{
		Space: rejecting,
		Blob:  blob.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}))
	require.NoError(t, w.accepts.Put(t.Context(), acceptance.Acceptance{
		Space: accepted,
		Blob:  acceptance.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}))
	w.pieces.Put(digest, []byte("data"))

	require.NoError(t, Reject(t.Context(), w.deps, &RejectRequest{Space: rejecting, Digest: digest}),
		"another space's acceptance must not block the reject")

	_, err := w.allocs.Get(t.Context(), digest, rejecting)
	require.ErrorIs(t, err, store.ErrNotFound, "rejecting space's allocation deleted")
	_, err = w.accepts.Get(t.Context(), digest, accepted)
	require.NoError(t, err, "other space's acceptance untouched")
	require.Empty(t, w.pieces.Removed(), "bytes retained for the accepting space")
}

func TestReject_OtherSpaceAllocationRetainsBytes(t *testing.T) {
	w := newRejectWorld(t)
	digest := testutil.RandomMultihash(t)
	abandoning := testutil.RandomDID(t)
	uploading := testutil.RandomDID(t)

	require.NoError(t, w.allocs.Put(t.Context(), allocation.Allocation{
		Space: abandoning,
		Blob:  blob.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}))
	require.NoError(t, w.allocs.Put(t.Context(), allocation.Allocation{
		Space: uploading,
		Blob:  blob.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}))
	w.pieces.Put(digest, []byte("data"))

	require.NoError(t, Reject(t.Context(), w.deps, &RejectRequest{Space: abandoning, Digest: digest}))

	_, err := w.allocs.Get(t.Context(), digest, abandoning)
	require.ErrorIs(t, err, store.ErrNotFound, "abandoning space's allocation deleted")
	_, err = w.allocs.Get(t.Context(), digest, uploading)
	require.NoError(t, err, "other space's allocation retained")
	require.Empty(t, w.pieces.Removed(), "shared bytes retained while another allocation lives")

	// The last allocation going releases the bytes.
	require.NoError(t, Reject(t.Context(), w.deps, &RejectRequest{Space: uploading, Digest: digest}))
	require.Len(t, w.pieces.Removed(), 1)
}
