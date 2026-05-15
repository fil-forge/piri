package blob

import (
	"context"
	"testing"

	"github.com/fil-forge/go-libstoracha/capabilities/assert"
	libtypes "github.com/fil-forge/go-libstoracha/capabilities/types"
	libtestutil "github.com/fil-forge/go-libstoracha/testutil"
	"github.com/fil-forge/go-ucanto/core/delegation"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/internal/testutil/pdpfake"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/delegationstore"
)

// stubPublisher is a no-op publisher.Publisher used for unit tests.
type stubPublisher struct {
	published []delegation.Delegation
}

func (s *stubPublisher) Publish(_ context.Context, d delegation.Delegation) error {
	s.published = append(s.published, d)
	return nil
}

func newAcceptDeps(t *testing.T) (AcceptDeps, *pdpfake.Pieces, *acceptancestore.Store, *delegationstore.Store, *stubPublisher) {
	t.Helper()
	ds := dssync.MutexWrap(datastore.NewMapDatastore())
	accepts := acceptancestore.NewDatastoreStore(ds)
	claimStore := delegationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
	pub := &stubPublisher{}
	pieces := pdpfake.NewPieces()
	return AcceptDeps{
		ID:          libtestutil.Alice,
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
		Space: libtestutil.RandomDID(t),
		Blob:  libtypes.Blob{Digest: libtestutil.RandomMultihash(t), Size: 4},
		Cause: libtestutil.RandomCID(t),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "piece not found")
}

func TestAccept_PieceHeld(t *testing.T) {
	deps, pieces, accepts, claimStore, pub := newAcceptDeps(t)

	digest := libtestutil.RandomMultihash(t)
	space := libtestutil.RandomDID(t)
	cause := libtestutil.RandomCID(t)
	pieces.Put(digest, []byte("data"))

	resp, err := Accept(t.Context(), deps, &AcceptRequest{
		Space: space,
		Blob:  libtypes.Blob{Digest: digest, Size: 4},
		Cause: cause,
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Claim, "location claim returned")
	require.NotNil(t, resp.PDP, "pdp/accept invocation returned")

	// acceptance persisted with the pdp/accept promise
	stored, err := accepts.Get(t.Context(), digest, space)
	require.NoError(t, err)
	require.Equal(t, digest, stored.Blob.Digest)
	require.Equal(t, space, stored.Space)
	require.Equal(t, cause, stored.Cause)
	require.NotNil(t, stored.PDPAccept, "pdp accept promise persisted")

	// claim persisted and published
	got, err := claimStore.Get(t.Context(), resp.Claim.Link())
	require.NoError(t, err)
	require.Equal(t, assert.LocationAbility, got.Capabilities()[0].Can())

	require.Len(t, pub.published, 1, "publisher invoked once")
	require.Equal(t, resp.Claim.Link(), pub.published[0].Link())
}
