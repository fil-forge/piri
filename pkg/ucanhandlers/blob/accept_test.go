package blob

import (
	"context"
	"testing"

	"github.com/fil-forge/libforge/commands/assert"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
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
		ID:          testutil.Alice,
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
	require.NotNil(t, stored.PDPAccept, "pdp accept promise persisted")

	// Claim persisted under its own link and is a /assert/location invocation.
	got, err := claimStore.Get(t.Context(), resp.Claim.Link())
	require.NoError(t, err)
	require.Equal(t, assert.Location.Command, got.Command())

	// Publisher invoked exactly once with the same claim.
	require.Len(t, pub.published, 1, "publisher invoked once")
	require.Equal(t, resp.Claim.Link(), pub.published[0].Link())
}
