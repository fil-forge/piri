package blob

import (
	"context"
	"testing"

	"github.com/fil-forge/libforge/commands/assert"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/libforge/testutil"
	ucanerrors "github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/internal/testutil/pdpfake"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
)

// stubPublisher is a no-op publisher.Publisher used for unit tests.
type stubPublisher struct {
	published []ucan.Invocation
}

func (s *stubPublisher) Publish(_ context.Context, inv ucan.Invocation) error {
	s.published = append(s.published, inv)
	return nil
}

func newAcceptDeps(t *testing.T) (AcceptDeps, *pdpfake.Pieces, *acceptancestore.Store, *invocationstore.Store, *stubPublisher) {
	t.Helper()
	ds := dssync.MutexWrap(datastore.NewMapDatastore())
	accepts := acceptancestore.NewDatastoreStore(ds)
	claimStore := invocationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
	pub := &stubPublisher{}
	pieces := pdpfake.NewPieces()
	allocs := allocationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
	return AcceptDeps{
		ID:          identity.Identity{Issuer: testutil.WebService},
		Acceptances: accepts,
		Pieces:      pieces,
		Commp:       pdpfake.NewCommp(),
		ClaimStore:  claimStore,
		Publisher:   pub,
		Pending:     allocs,
		Tx:          store.NoTransaction{},
	}, pieces, accepts, claimStore, pub
}

// requireNamedFailure checks err is the named failure a receipt reports.
func requireNamedFailure(t *testing.T, err error, name string) {
	t.Helper()
	var named ucanerrors.Named
	require.True(t, ucanerrors.As(err, &named), "want a named failure, got %v", err)
	require.Equal(t, name, named.Name())
}

// storedAllocation gives a random space an allocation of a random blob that
// expires at expires.
func storedAllocation(t *testing.T, deps AcceptDeps, expires ucan.UnixTimestamp) allocation.Allocation {
	t.Helper()
	alloc := allocation.Allocation{
		Space:      testutil.RandomDID(t),
		Blob:       blob.Blob{Digest: testutil.RandomMultihash(t), Size: 4},
		Expires:    expires,
		Cause:      testutil.RandomCID(t),
		Allocation: testutil.RandomCID(t),
	}
	require.NoError(t, deps.Pending.(*allocationstore.Store).Put(t.Context(), alloc))
	return alloc
}

// Bytes that are not held, in a space with no allocation of the blob.
func TestAccept_PieceMissingWithoutAllocation(t *testing.T) {
	deps, _, _, _, _ := newAcceptDeps(t)

	_, err := Accept(t.Context(), deps, &AcceptRequest{
		Space: testutil.RandomDID(t),
		Blob:  blob.Blob{Digest: testutil.RandomMultihash(t), Size: 4},
		Cause: testutil.RandomCID(t),
	})
	requireNamedFailure(t, err, blob.AllocationNotFoundErrorName)
}

// Bytes not yet delivered to an allocation that has not expired: the accept
// fails without a named failure, so it can be sent again.
func TestAccept_PieceMissingBeforeExpiry(t *testing.T) {
	deps, _, _, _, _ := newAcceptDeps(t)
	alloc := storedAllocation(t, deps, ucan.Now()+3600)

	_, err := Accept(t.Context(), deps, &AcceptRequest{
		Space:      alloc.Space,
		Blob:       alloc.Blob,
		Cause:      testutil.RandomCID(t),
		Allocation: &alloc.Allocation,
	})
	require.ErrorContains(t, err, "piece not found")
	var named ucanerrors.Named
	require.False(t, ucanerrors.As(err, &named), "not a named failure: %v", err)
}

// Bytes never delivered to an allocation that has expired, whether or not the
// accept identifies the allocation.
func TestAccept_PieceMissingAfterExpiry(t *testing.T) {
	for name, identified := range map[string]bool{"allocation identified": true, "allocation not identified": false} {
		t.Run(name, func(t *testing.T) {
			deps, _, accepts, _, _ := newAcceptDeps(t)
			alloc := storedAllocation(t, deps, ucan.Now()-1)
			req := &AcceptRequest{Space: alloc.Space, Blob: alloc.Blob, Cause: testutil.RandomCID(t)}
			if identified {
				req.Allocation = &alloc.Allocation
			}

			_, err := Accept(t.Context(), deps, req)
			requireNamedFailure(t, err, blob.AllocationExpiredErrorName)

			_, err = accepts.Get(t.Context(), alloc.Blob.Digest, alloc.Space)
			require.ErrorIs(t, err, store.ErrNotFound, "no acceptance is recorded")
		})
	}
}

// An allocation past its expiry whose bytes were delivered is accepted.
func TestAccept_PieceHeldAfterExpiry(t *testing.T) {
	deps, pieces, accepts, _, _ := newAcceptDeps(t)
	alloc := storedAllocation(t, deps, ucan.Now()-1)
	pieces.Put(alloc.Blob.Digest, []byte("data"))

	_, err := Accept(t.Context(), deps, &AcceptRequest{
		Space:      alloc.Space,
		Blob:       alloc.Blob,
		Cause:      testutil.RandomCID(t),
		Allocation: &alloc.Allocation,
	})
	require.NoError(t, err)
	_, err = accepts.Get(t.Context(), alloc.Blob.Digest, alloc.Space)
	require.NoError(t, err)
}

// A blob the space holds no allocation of is refused even though the bytes
// are held, whether or not the accept identifies an allocation.
func TestAccept_PieceHeldWithoutAllocation(t *testing.T) {
	for name, identified := range map[string]bool{"allocation identified": true, "allocation not identified": false} {
		t.Run(name, func(t *testing.T) {
			deps, pieces, accepts, _, _ := newAcceptDeps(t)
			digest, space, link := testutil.RandomMultihash(t), testutil.RandomDID(t), testutil.RandomCID(t)
			pieces.Put(digest, []byte("data"))
			req := &AcceptRequest{Space: space, Blob: blob.Blob{Digest: digest, Size: 4}, Cause: testutil.RandomCID(t)}
			if identified {
				req.Allocation = &link
			}

			_, err := Accept(t.Context(), deps, req)
			requireNamedFailure(t, err, blob.AllocationNotFoundErrorName)
			_, err = accepts.Get(t.Context(), digest, space)
			require.ErrorIs(t, err, store.ErrNotFound, "no acceptance is recorded")
		})
	}
}

func TestAccept_PieceHeld(t *testing.T) {
	deps, pieces, accepts, claimStore, pub := newAcceptDeps(t)

	alloc := storedAllocation(t, deps, ucan.Now()+3600)
	digest, space := alloc.Blob.Digest, alloc.Space
	cause := testutil.RandomCID(t)
	pieces.Put(digest, []byte("data"))

	resp, err := Accept(t.Context(), deps, &AcceptRequest{
		Space: space,
		Blob:  alloc.Blob,
		Cause: cause,
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Claim, "location claim returned")
	require.NotNil(t, resp.PDP, "pdp/accept invocation returned")

	// Acceptance persisted with the pdp/accept promise.
	stored, err := accepts.Get(t.Context(), digest, space)
	require.NoError(t, err)
	require.Equal(t, digest, stored.Blob.Digest)
	require.Equal(t, space, stored.Space)
	require.Equal(t, cause, stored.Cause)
	require.Equal(t, resp.PDP.Task().Link(), stored.PDPAccept.Task, "pdp accept promise persisted")
	require.Equal(t, resp.Claim.Link(), stored.Site, "location claim link persisted")

	// Claim persisted under its own link and is a /assert/location invocation.
	got, err := claimStore.Get(t.Context(), resp.Claim.Link())
	require.NoError(t, err)
	require.Equal(t, assert.Location.Command, got.Command())

	// Publisher invoked exactly once with the same claim.
	require.Len(t, pub.published, 1, "publisher invoked once")
	require.Equal(t, resp.Claim.Link(), pub.published[0].Link())
}

// orderProbeCommp observes, at Enqueue time, whether the digest's acceptance
// is already written, so the pipeline entry never precedes it.
type orderProbeCommp struct {
	accepts             *acceptancestore.Store
	acceptanceAtEnqueue bool
	enqueued            int
}

func (o *orderProbeCommp) Enqueue(ctx context.Context, blob multihash.Multihash) error {
	o.enqueued++
	exists, err := o.accepts.Exists(ctx, blob)
	if err != nil {
		return err
	}
	o.acceptanceAtEnqueue = exists
	return nil
}

func TestAccept_AcceptanceDurableBeforeEnqueue(t *testing.T) {
	deps, pieces, accepts, _, _ := newAcceptDeps(t)
	probe := &orderProbeCommp{accepts: accepts}
	deps.Commp = probe

	alloc := storedAllocation(t, deps, ucan.Now()+3600)
	pieces.Put(alloc.Blob.Digest, []byte("data"))

	_, err := Accept(t.Context(), deps, &AcceptRequest{
		Space: alloc.Space,
		Blob:  alloc.Blob,
		Cause: testutil.RandomCID(t),
	})
	require.NoError(t, err)
	require.Equal(t, 1, probe.enqueued)
	require.True(t, probe.acceptanceAtEnqueue,
		"the acceptance is written before the pipeline enqueue")
}
