package rpc_test

import (
	"testing"

	"github.com/fil-forge/libforge/commands/assert"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store"
)

// /blob/release is performed by the upload service under a
// provider->upload-service delegation, mirroring /blob/accept: the subject is
// the storage provider and the space releasing its claim travels in the
// arguments. The release carries Cause, a link to the originating
// /blob/remove task; the remove envelope ships in the request container and
// the handler verifies it (right command, subject == space, digest match,
// valid proof chain rooted at the space) before dropping the claim. The
// handler deletes the space's allocation, acceptance, and location claim,
// and releases the bytes only when no space claims the digest afterward.

// selfIssuedRemove builds the /blob/remove invocation a space issues to the
// upload service. Issuer == subject == the space, so the validator accepts
// it on root authority without a proof chain.
func (s *RPCSuite) selfIssuedRemove(t *testing.T, space ucan.Issuer, digest multihash.Multihash) ucan.Invocation {
	t.Helper()
	return testutil.Must(blob.Remove.Invoke(
		space,
		space.DID(),
		&blob.RemoveArguments{Digest: digest},
		invocation.WithAudience(s.UploadServiceIdentity.DID()),
	))(t)
}

// sendRelease invokes /blob/release as the upload service (subject = the
// provider) with the given cause link, shipping the cause envelopes and any
// extra proof delegations in the request container. Pass nil causes to
// exercise the missing-envelope failure path.
func (s *RPCSuite) sendRelease(
	t *testing.T,
	space did.DID,
	digest multihash.Multihash,
	causeLink cid.Cid,
	causes []ucan.Invocation,
	extraProofs ...ucan.Delegation,
) ucan.Receipt {
	t.Helper()
	service := s.ServiceID.DID()

	releaseProof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), service, blob.Release.Command,
	))(t)
	release := testutil.Must(blob.Release.Invoke(
		s.UploadServiceIdentity,
		service,
		&blob.ReleaseArguments{
			Space:  space,
			Digest: digest,
			Cause:  causeLink,
		},
		invocation.WithAudience(service),
		invocation.WithProofs(releaseProof.Link()),
	))(t)

	proofs := append([]ucan.Delegation{releaseProof}, extraProofs...)
	return s.sendInvocationWith(t, release, causes, proofs...)
}

// seedAcceptedBlob stores a random blob and accepts it for space via the
// /blob/accept flow, returning the digest. Gives the failure-path tests
// state to assert is retained.
func (s *RPCSuite) seedAcceptedBlob(t *testing.T, space did.DID) multihash.Multihash {
	t.Helper()
	service := s.ServiceID.DID()

	data := testutil.RandomBytes(t, 64)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	s.Pieces.Put(digest, data)

	acceptProof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), service, blob.Accept.Command,
	))(t)
	accept := testutil.Must(blob.Accept.Invoke(
		s.UploadServiceIdentity,
		service,
		&blob.AcceptArguments{
			Space: space,
			Blob:  blob.Blob{Digest: digest, Size: uint64(len(data))},
			Put:   promise.AwaitOK{Task: testutil.RandomCID(t)},
		},
		invocation.WithAudience(service),
		invocation.WithProofs(acceptProof.Link()),
	))(t)
	assertReceiptOK(t, s.sendInvocationWithProofs(t, accept, acceptProof))
	return digest
}

// assertClaimRetained asserts a failed release left the space's acceptance
// in place and did not queue the bytes for removal.
func (s *RPCSuite) assertClaimRetained(t *testing.T, space did.DID, digest multihash.Multihash) {
	t.Helper()
	_, err := s.Acceptances.Get(t.Context(), digest, space)
	require.NoError(t, err, "acceptance retained")
	require.NotContains(t, s.Pieces.Removed(), digest, "bytes retained")
}

func (s *RPCSuite) TestBlobRelease_ReleasesClaimAndBytes() {
	t := s.T()
	service := s.ServiceID.DID()
	spaceSigner := testutil.Bob
	space := spaceSigner.DID()

	// Store a blob the usual way: allocate, upload, accept.
	data := testutil.RandomBytes(t, 64)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	size := uint64(len(data))

	alloc := testutil.Must(blob.Allocate.Invoke(
		s.ServiceID,
		service,
		&blob.AllocateArguments{
			Space: space,
			Blob:  blob.Blob{Digest: digest, Size: size},
			Cause: testutil.RandomCID(t),
		},
		invocation.WithAudience(service),
	))(t)
	assertReceiptOK(t, s.sendInvocation(t, alloc))
	s.Pieces.Put(digest, data)

	acceptProof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), service, blob.Accept.Command,
	))(t)
	accept := testutil.Must(blob.Accept.Invoke(
		s.UploadServiceIdentity,
		service,
		&blob.AcceptArguments{
			Space: space,
			Blob:  blob.Blob{Digest: digest, Size: size},
			Put:   promise.AwaitOK{Task: testutil.RandomCID(t)},
		},
		invocation.WithAudience(service),
		invocation.WithProofs(acceptProof.Link()),
	))(t)
	acceptOK := decodeAcceptOK(t, s.sendInvocationWithProofs(t, accept, acceptProof))

	// Remove the blob from the space, citing the space's /blob/remove.
	remove := s.selfIssuedRemove(t, spaceSigner, digest)
	assertReceiptOK(t, s.sendRelease(t, space, digest, remove.Task().Link(), []ucan.Invocation{remove}))

	// All three per-space records are gone.
	_, err := s.Allocations.Get(t.Context(), digest, space)
	require.ErrorIs(t, err, store.ErrNotFound, "allocation deleted")
	_, err = s.Acceptances.Get(t.Context(), digest, space)
	require.ErrorIs(t, err, store.ErrNotFound, "acceptance deleted")
	_, err = s.ClaimStore.Get(t.Context(), acceptOK.Site)
	require.ErrorIs(t, err, store.ErrNotFound, "location claim deleted")

	// No space claims the digest anymore — byte release was requested.
	require.Contains(t, s.Pieces.Removed(), digest)

	// Idempotent: removing again succeeds.
	assertReceiptOK(t, s.sendRelease(t, space, digest, remove.Task().Link(), []ucan.Invocation{remove}))
}

func (s *RPCSuite) TestBlobRelease_OtherSpaceRetainsBytes() {
	t := s.T()
	spaceSignerA := testutil.Bob
	spaceA := spaceSignerA.DID()
	spaceB := testutil.Mallory.DID()

	data := testutil.RandomBytes(t, 64)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	size := uint64(len(data))
	service := s.ServiceID.DID()

	s.Pieces.Put(digest, data)
	acceptProof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), service, blob.Accept.Command,
	))(t)
	for _, space := range []did.DID{spaceA, spaceB} {
		accept := testutil.Must(blob.Accept.Invoke(
			s.UploadServiceIdentity,
			service,
			&blob.AcceptArguments{
				Space: space,
				Blob:  blob.Blob{Digest: digest, Size: size},
				Put:   promise.AwaitOK{Task: testutil.RandomCID(t)},
			},
			invocation.WithAudience(service),
			invocation.WithProofs(acceptProof.Link()),
		))(t)
		assertReceiptOK(t, s.sendInvocationWithProofs(t, accept, acceptProof))
	}

	// Remove from spaceA only.
	remove := s.selfIssuedRemove(t, spaceSignerA, digest)
	assertReceiptOK(t, s.sendRelease(t, spaceA, digest, remove.Task().Link(), []ucan.Invocation{remove}))

	_, err := s.Acceptances.Get(t.Context(), digest, spaceA)
	require.ErrorIs(t, err, store.ErrNotFound, "spaceA's acceptance deleted")
	_, err = s.Acceptances.Get(t.Context(), digest, spaceB)
	require.NoError(t, err, "spaceB's acceptance retained")
	require.NotContains(t, s.Pieces.Removed(), digest,
		"spaceB still claims the digest — bytes retained")
}

func (s *RPCSuite) TestBlobRelease_DelegatedRemove() {
	t := s.T()
	spaceSigner := testutil.Bob
	space := spaceSigner.DID()
	digest := s.seedAcceptedBlob(t, space)

	// The space delegates /blob/remove to the upload service, which issues
	// the remove on the space's behalf. The handler's validator walks the
	// proof chain shipped in the request container and accepts.
	removeDlg := testutil.Must(delegation.Delegate(
		spaceSigner, s.UploadServiceIdentity.DID(), space, blob.Remove.Command,
	))(t)
	remove := testutil.Must(blob.Remove.Invoke(
		s.UploadServiceIdentity,
		space,
		&blob.RemoveArguments{Digest: digest},
		invocation.WithAudience(s.UploadServiceIdentity.DID()),
		invocation.WithProofs(removeDlg.Link()),
	))(t)

	assertReceiptOK(t, s.sendRelease(t, space, digest, remove.Task().Link(), []ucan.Invocation{remove}, removeDlg))

	_, err := s.Acceptances.Get(t.Context(), digest, space)
	require.ErrorIs(t, err, store.ErrNotFound, "acceptance deleted")
	require.Contains(t, s.Pieces.Removed(), digest)
}

func (s *RPCSuite) TestBlobRelease_UnknownCause_MissingEnvelope() {
	t := s.T()
	spaceSigner := testutil.Bob
	space := spaceSigner.DID()
	digest := s.seedAcceptedBlob(t, space)

	// Cause links a real /blob/remove, but the envelope is not shipped in
	// the request container — the handler can't resolve it.
	remove := s.selfIssuedRemove(t, spaceSigner, digest)
	rcpt := s.sendRelease(t, space, digest, remove.Task().Link(), nil)
	assertReceiptFailure(t, rcpt, blob.UnknownCauseErrorName)
	s.assertClaimRetained(t, space, digest)
}

func (s *RPCSuite) TestBlobRelease_UnknownCause_WrongCommand() {
	t := s.T()
	spaceSigner := testutil.Bob
	space := spaceSigner.DID()
	digest := s.seedAcceptedBlob(t, space)

	// The linked cause is a valid invocation, but not a /blob/remove task.
	cause := testutil.Must(assert.Equals.Invoke(
		spaceSigner,
		space,
		&assert.EqualsArguments{
			Content: digest,
			Equals:  testutil.RandomCID(t),
		},
		invocation.WithAudience(s.UploadServiceIdentity.DID()),
	))(t)

	rcpt := s.sendRelease(t, space, digest, cause.Task().Link(), []ucan.Invocation{cause})
	assertReceiptFailure(t, rcpt, blob.UnknownCauseErrorName)
	s.assertClaimRetained(t, space, digest)
}

func (s *RPCSuite) TestBlobRelease_InvalidCause_SubjectMismatch() {
	t := s.T()
	space := testutil.Bob.DID()
	digest := s.seedAcceptedBlob(t, space)

	// The remove is rooted at a different space than the one the release
	// names — one space must not be able to drop another's claim.
	remove := s.selfIssuedRemove(t, testutil.Mallory, digest)
	rcpt := s.sendRelease(t, space, digest, remove.Task().Link(), []ucan.Invocation{remove})
	assertReceiptFailure(t, rcpt, blob.InvalidCauseErrorName)
	s.assertClaimRetained(t, space, digest)
}

func (s *RPCSuite) TestBlobRelease_InvalidCause_DigestMismatch() {
	t := s.T()
	spaceSigner := testutil.Bob
	space := spaceSigner.DID()
	digest := s.seedAcceptedBlob(t, space)

	// The remove names a different digest than the release.
	remove := s.selfIssuedRemove(t, spaceSigner, testutil.RandomMultihash(t))
	rcpt := s.sendRelease(t, space, digest, remove.Task().Link(), []ucan.Invocation{remove})
	assertReceiptFailure(t, rcpt, blob.InvalidCauseErrorName)
	s.assertClaimRetained(t, space, digest)
}

func (s *RPCSuite) TestBlobRelease_InvalidCause_Unauthorized() {
	t := s.T()
	space := testutil.Bob.DID()
	digest := s.seedAcceptedBlob(t, space)

	// The remove has the right shape (subject = space, digest matches) but
	// is issued by the upload service with no delegation from the space —
	// the handler's validator step rejects it.
	remove := testutil.Must(blob.Remove.Invoke(
		s.UploadServiceIdentity,
		space,
		&blob.RemoveArguments{Digest: digest},
		invocation.WithAudience(s.UploadServiceIdentity.DID()),
		// intentionally no invocation.WithProofs(...)
	))(t)

	rcpt := s.sendRelease(t, space, digest, remove.Task().Link(), []ucan.Invocation{remove})
	assertReceiptFailure(t, rcpt, blob.InvalidCauseErrorName)
	s.assertClaimRetained(t, space, digest)
}
