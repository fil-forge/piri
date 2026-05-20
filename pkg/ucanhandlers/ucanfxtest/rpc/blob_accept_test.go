package rpc_test

import (
	"bytes"
	"net/url"

	"github.com/fil-forge/libforge/commands/assert"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

// /blob/accept (like /blob/allocate) requires invocation subject == service
// DID (per pkg/ucanhandlers/blob/accept.go:75) and reads the space from the
// invocation subject. The handler:
//   - looks up the blob bytes in the piece store (Has check)
//   - writes an acceptance record carrying a /pdp/accept promise
//   - issues a /assert/location claim and persists it in the invocation store
//   - publishes the claim to IPNI
// Tests assert the receipt + all three side effects.

func (s *RPCSuite) TestBlobAccept_Basic() {
	t := s.T()

	// Seed bytes the handler will read back when shaping the location
	// claim. We use real bytes so digest, content-length, etc. are
	// internally consistent.
	data := testutil.RandomBytes(t, 64)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	size := uint64(len(data))
	s.Pieces.Put(digest, data)

	// The Put promise on AcceptArguments is an await on the blob/allocate
	// receipt's site link in production; for an isolated accept test we
	// hand it a freshly-minted CID — the handler doesn't resolve the
	// promise, it just persists it alongside the acceptance record.
	putAwait := promise.AwaitOK{Task: testutil.RandomCID(t)}

	inv := testutil.Must(blob.Accept.Invoke(
		s.ServiceID,
		s.ServiceID.DID(),
		&blob.AcceptArguments{
			Blob: blob.Blob{Digest: digest, Size: size},
			Put:  putAwait,
		},
		invocation.WithAudience(s.ServiceID.DID()),
	))(t)

	rcpt := s.sendInvocation(t, inv)
	ok := decodeAcceptOK(t, rcpt)
	require.NotEqual(t, cid.Undef, ok.Site, "AcceptOK.Site is the location claim CID")

	// Acceptance store carries the accepted blob + space + cause.
	acc, err := s.Acceptances.Get(t.Context(), digest, s.ServiceID.DID())
	require.NoError(t, err, "acceptance persisted")
	require.Equal(t, digest, acc.Blob.Digest)
	require.Equal(t, size, acc.Blob.Size)
	require.Equal(t, s.ServiceID.DID(), acc.Space)
	require.Equal(t, inv.Link(), acc.Cause, "cause records the accept invocation link")
	require.NotNil(t, acc.PDPAccept, "PDP accept promise is recorded for aggregation completion")

	// Invocation store carries the location commitment under AcceptOK.Site.
	claim, err := s.ClaimStore.Get(t.Context(), ok.Site)
	require.NoError(t, err, "location claim persisted under Site CID")
	require.Equal(t, s.ServiceID.DID(), claim.Issuer(), "claim is signed by the service")
	require.Equal(t, s.ServiceID.DID(), claim.Subject(),
		"location claim is scoped to the service in the current single-space handler")
	require.Equal(t, assert.Location.Command, claim.Command(),
		"claim is an /assert/location invocation")

	// Args carry the digest the test stored and a location URL matching
	// what pdpfake.ReadPieceURL would hand back for the same blob CID.
	var locArgs assert.LocationArguments
	require.NoError(t, locArgs.UnmarshalCBOR(bytes.NewReader(claim.ArgumentsBytes())),
		"location claim args decode")
	require.Equal(t, digest, locArgs.Content, "claim references the accepted blob digest")
	expectedURL, err := s.Pieces.ReadPieceURL(cid.NewCidV1(cid.Raw, digest))
	require.NoError(t, err)
	require.Len(t, locArgs.Location, 1)
	gotURL := url.URL(locArgs.Location[0])
	require.Equal(t, expectedURL.String(), gotURL.String(),
		"claim points at the URL the piece store would serve")
}

// TestBlobAccept_ExistingDataInDifferentSpace verifies the per-space
// allocation/acceptance model: the same physical blob (same digest +
// bytes) can be accepted independently into two different spaces.
// Each space gets its own allocation and acceptance record keyed on
// (digest, space); the location commitment is issued separately for
// each, even though the underlying bytes are shared in the piece store.
//
// This test does NOT exercise replica/transfer — that flow re-shapes
// data on disk and is still gated behind the commented-out
// replica.Module. What's tested here is the simpler deduplication
// path: once a blob exists in space A's pipeline, space B can allocate
// + accept the same blob with no fresh upload.
func (s *RPCSuite) TestBlobAccept_ExistingDataInDifferentSpace() {
	t := s.T()
	service := s.ServiceID.DID()

	data := testutil.RandomBytes(t, 64)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	size := uint64(len(data))

	// Two spaces, each its own did:key signer. Each signs invocations
	// against itself as subject — issuer == subject, no proof chain
	// required for the validator.
	spaceA := testutil.RandomSigner(t)
	spaceB := testutil.RandomSigner(t)

	// --- spaceA: first allocation, then upload, then accept ---

	allocA := testutil.Must(blob.Allocate.Invoke(
		spaceA,
		spaceA.DID(),
		&blob.AllocateArguments{
			Blob:  blob.Blob{Digest: digest, Size: size},
			Cause: testutil.RandomCID(t),
		},
		invocation.WithAudience(service),
	))(t)
	okA := decodeAllocateOK(t, s.sendInvocation(t, allocA))
	require.Equal(t, size, okA.Size, "first allocation reserves full size")
	require.NotNil(t, okA.Address, "first allocation returns an upload URL")

	// Simulate the upload completing.
	s.Pieces.Put(digest, data)

	acceptA := testutil.Must(blob.Accept.Invoke(
		spaceA,
		spaceA.DID(),
		&blob.AcceptArguments{
			Blob: blob.Blob{Digest: digest, Size: size},
			Put:  promise.AwaitOK{Task: testutil.RandomCID(t)},
		},
		invocation.WithAudience(service),
	))(t)
	acceptOKA := decodeAcceptOK(t, s.sendInvocation(t, acceptA))

	// --- spaceB: bytes are already present from spaceA's upload ---

	allocB := testutil.Must(blob.Allocate.Invoke(
		spaceB,
		spaceB.DID(),
		&blob.AllocateArguments{
			Blob:  blob.Blob{Digest: digest, Size: size},
			Cause: testutil.RandomCID(t),
		},
		invocation.WithAudience(service),
	))(t)
	okB := decodeAllocateOK(t, s.sendInvocation(t, allocB))
	require.Equal(t, size, okB.Size,
		"different space gets its own fresh allocation accounting")
	require.Nil(t, okB.Address,
		"bytes already in store from spaceA — no upload URL for spaceB")

	acceptB := testutil.Must(blob.Accept.Invoke(
		spaceB,
		spaceB.DID(),
		&blob.AcceptArguments{
			Blob: blob.Blob{Digest: digest, Size: size},
			Put:  promise.AwaitOK{Task: testutil.RandomCID(t)},
		},
		invocation.WithAudience(service),
	))(t)
	acceptOKB := decodeAcceptOK(t, s.sendInvocation(t, acceptB))

	// --- both spaces have independent records keyed on (digest, space) ---

	for _, sp := range []struct {
		signer ucan.Signer
		site   cid.Cid
		cause  cid.Cid
	}{
		{spaceA, acceptOKA.Site, acceptA.Link()},
		{spaceB, acceptOKB.Site, acceptB.Link()},
	} {
		alloc, err := s.Allocations.Get(t.Context(), digest, sp.signer.DID())
		require.NoError(t, err, "allocation persisted under space=%s", sp.signer.DID())
		require.Equal(t, sp.signer.DID(), alloc.Space)
		require.Equal(t, digest, alloc.Blob.Digest)

		acc, err := s.Acceptances.Get(t.Context(), digest, sp.signer.DID())
		require.NoError(t, err, "acceptance persisted under space=%s", sp.signer.DID())
		require.Equal(t, sp.signer.DID(), acc.Space)
		require.Equal(t, sp.cause, acc.Cause)

		claim, err := s.ClaimStore.Get(t.Context(), sp.site)
		require.NoError(t, err, "location claim persisted under Site CID")
		require.Equal(t, sp.signer.DID(), claim.Subject(),
			"location claim is scoped to this space")
	}

	// The two acceptances issued distinct location commitments.
	require.NotEqual(t, acceptOKA.Site, acceptOKB.Site,
		"each space's accept issues a distinct location commitment")
}
