package rpc_test

import (
	"bytes"
	"net/url"

	"github.com/fil-forge/libforge/commands/assert"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

// /blob/accept is performed by the upload service: The invocation subject is
// the storage provider (authorized by a provider->upload-service delegation)
// and the space travels in AcceptArguments.Space. The handler:
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
	space := testutil.RandomDID(t)

	// /blob/accept is performed by the upload service: the provider delegates
	// /blob/accept to the upload service, which issues the invocation with the
	// provider as its subject and the delegation as proof. The space travels in
	// the arguments.
	proof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), s.ServiceID.DID(), blob.Accept.Command,
	))(t)
	inv := testutil.Must(blob.Accept.Invoke(
		s.UploadServiceIdentity,
		s.ServiceID.DID(),
		&blob.AcceptArguments{
			Space: space,
			Blob:  blob.Blob{Digest: digest, Size: size},
			Put:   putAwait,
		},
		invocation.WithAudience(s.ServiceID.DID()),
		invocation.WithProofs(proof.Link()),
	))(t)

	rcpt := s.sendInvocationWithProofs(t, inv, proof)
	ok := decodeAcceptOK(t, rcpt)
	require.NotEqual(t, cid.Undef, ok.Site, "AcceptOK.Site is the location claim CID")

	// Acceptance store carries the accepted blob + space + cause.
	acc, err := s.Acceptances.Get(t.Context(), digest, space)
	require.NoError(t, err, "acceptance persisted")
	require.Equal(t, digest, acc.Blob.Digest)
	require.Equal(t, size, acc.Blob.Size)
	require.Equal(t, space, acc.Space)
	require.Equal(t, inv.Task().Link(), acc.Cause, "cause records the accept task link")
	require.NotEqual(t, cid.Undef, acc.PDPAccept.Task, "PDP accept promise is recorded for aggregation completion")
	require.Equal(t, ok.Site, acc.Site, "acceptance records the location claim link AcceptOK returns")

	// Invocation store carries the location commitment under AcceptOK.Site.
	claim, err := s.ClaimStore.Get(t.Context(), ok.Site)
	require.NoError(t, err, "location claim persisted under Site CID")
	require.Equal(t, s.ServiceID.DID(), claim.Issuer(), "claim is signed by the service")
	require.Equal(t, s.ServiceID.DID(), claim.Subject(),
		"location claim is issued by the provider node")
	require.Equal(t, assert.Location.Command, claim.Command(),
		"claim is an /assert/location invocation")

	// Args carry the digest the test stored and a location URL matching
	// what pdpfake.ReadPieceURL would hand back for the same blob CID.
	var locArgs assert.LocationArguments
	require.NoError(t, locArgs.UnmarshalCBOR(bytes.NewReader(claim.ArgumentsBytes())),
		"location claim args decode")
	require.Equal(t, digest, locArgs.Content, "claim references the accepted blob digest")
	require.Equal(t, space, locArgs.Space, "location claim is scoped to the space from the args")
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

	// Two spaces, each just a DID carried in the invocation arguments. Every
	// invocation's subject is the provider. /blob/allocate is self-issued by the
	// provider (issuer == subject, no proof chain); /blob/accept is issued by the
	// upload service and authorized by a single provider->upload-service
	// delegation, reused across spaces since it is not space-scoped.
	spaceA := testutil.RandomDID(t)
	spaceB := testutil.RandomDID(t)

	acceptProof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), service, blob.Accept.Command,
	))(t)

	// --- spaceA: first allocation, then upload, then accept ---

	allocA := testutil.Must(blob.Allocate.Invoke(
		s.ServiceID,
		service,
		&blob.AllocateArguments{
			Space: spaceA,
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
		s.UploadServiceIdentity,
		service,
		&blob.AcceptArguments{
			Space: spaceA,
			Blob:  blob.Blob{Digest: digest, Size: size},
			Put:   promise.AwaitOK{Task: testutil.RandomCID(t)},
		},
		invocation.WithAudience(service),
		invocation.WithProofs(acceptProof.Link()),
	))(t)
	acceptOKA := decodeAcceptOK(t, s.sendInvocationWithProofs(t, acceptA, acceptProof))

	// --- spaceB: bytes are already present from spaceA's upload ---

	allocB := testutil.Must(blob.Allocate.Invoke(
		s.ServiceID,
		service,
		&blob.AllocateArguments{
			Space: spaceB,
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
		s.UploadServiceIdentity,
		service,
		&blob.AcceptArguments{
			Space: spaceB,
			Blob:  blob.Blob{Digest: digest, Size: size},
			Put:   promise.AwaitOK{Task: testutil.RandomCID(t)},
		},
		invocation.WithAudience(service),
		invocation.WithProofs(acceptProof.Link()),
	))(t)
	acceptOKB := decodeAcceptOK(t, s.sendInvocationWithProofs(t, acceptB, acceptProof))

	// --- both spaces have independent records keyed on (digest, space) ---

	for _, sp := range []struct {
		space did.DID
		site  cid.Cid
		cause cid.Cid
	}{
		{spaceA, acceptOKA.Site, acceptA.Task().Link()},
		{spaceB, acceptOKB.Site, acceptB.Task().Link()},
	} {
		alloc, err := s.Allocations.Get(t.Context(), digest, sp.space)
		require.NoError(t, err, "allocation persisted under space=%s", sp.space)
		require.Equal(t, sp.space, alloc.Space)
		require.Equal(t, digest, alloc.Blob.Digest)

		acc, err := s.Acceptances.Get(t.Context(), digest, sp.space)
		require.NoError(t, err, "acceptance persisted under space=%s", sp.space)
		require.Equal(t, sp.space, acc.Space)
		require.Equal(t, sp.cause, acc.Cause)

		claim, err := s.ClaimStore.Get(t.Context(), sp.site)
		require.NoError(t, err, "location claim persisted under Site CID")

		var lcArgs assert.LocationArguments
		require.NoError(t, lcArgs.UnmarshalCBOR(bytes.NewReader(claim.ArgumentsBytes())))
		require.Equal(t, digest, lcArgs.Content, "claim references the accepted blob digest")
		require.Equal(t, sp.space, lcArgs.Space, "location claim is scoped to this space")
	}

	// The two acceptances issued distinct location commitments.
	require.NotEqual(t, acceptOKA.Site, acceptOKB.Site,
		"each space's accept issues a distinct location commitment")
}
