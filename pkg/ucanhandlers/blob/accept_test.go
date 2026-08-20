package blob

import (
	"context"
	"errors"
	"testing"

	"github.com/fil-forge/libforge/commands/assert"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/internal/testutil/pdpfake"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
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
	return AcceptDeps{
		ID:          identity.Identity{Issuer: testutil.WebService},
		Acceptances: accepts,
		Pieces:      pieces,
		Commp:       pdpfake.NewCommp(),
		ClaimStore:  claimStore,
		Publisher:   pub,
	}, pieces, accepts, claimStore, pub
}

func TestAccept_PieceMissing(t *testing.T) {
	deps, _, _, _, _ := newAcceptDeps(t)

	_, err := Accept(t.Context(), deps, &AcceptRequest{
		Space: testutil.RandomDID(t),
		Blob:  blob.Blob{Digest: testutil.RandomMultihash(t), Size: 4},
		Cause: testutil.RandomCID(t),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "piece not found")
}

func TestAccept_PieceHeld(t *testing.T) {
	deps, pieces, accepts, claimStore, pub := newAcceptDeps(t)

	digest := testutil.RandomMultihash(t)
	space := testutil.RandomDID(t)
	cause := testutil.RandomCID(t)
	pieces.Put(digest, []byte("data"))

	resp, err := Accept(t.Context(), deps, &AcceptRequest{
		Space: space,
		Blob:  blob.Blob{Digest: digest, Size: 4},
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
// is already durable — the ordering /blob/reject's BlobAccepted guard
// and the removal sweep's claim re-checks depend on.
type orderProbeCommp struct {
	accepts             *acceptancestore.Store
	fail                bool
	acceptanceAtEnqueue bool
	enqueued            int
}

func (o *orderProbeCommp) Enqueue(ctx context.Context, blob multihash.Multihash) error {
	o.enqueued++
	if o.fail {
		return errors.New("enqueue boom")
	}
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

	digest := testutil.RandomMultihash(t)
	pieces.Put(digest, []byte("data"))

	_, err := Accept(t.Context(), deps, &AcceptRequest{
		Space: testutil.RandomDID(t),
		Blob:  blob.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	})
	require.NoError(t, err)
	require.Equal(t, 1, probe.enqueued)
	require.True(t, probe.acceptanceAtEnqueue,
		"the acceptance must be written before the pipeline enqueue")
}

func TestAccept_EnqueueFailureCompensatesAcceptance(t *testing.T) {
	deps, pieces, accepts, claimStore, pub := newAcceptDeps(t)
	probe := &orderProbeCommp{accepts: accepts, fail: true}
	deps.Commp = probe

	digest := testutil.RandomMultihash(t)
	pieces.Put(digest, []byte("data"))

	_, err := Accept(t.Context(), deps, &AcceptRequest{
		Space: testutil.RandomDID(t),
		Blob:  blob.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "enqueue boom")

	exists, cerr := accepts.Exists(t.Context(), digest)
	require.NoError(t, cerr)
	require.False(t, exists, "acceptance compensated away on enqueue failure")
	require.Empty(t, pub.published, "nothing published for a failed accept")
	_ = claimStore // no claim assertions: the claim is never stored (Put runs after enqueue)
}
