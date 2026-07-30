package rpc_test

import (
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store"
)

// /blob/release is performed by the upload service under a
// provider->upload-service delegation, mirroring /blob/accept: the subject is
// the storage provider and the space releasing its claim travels in the
// arguments. The handler deletes the space's allocation, acceptance, and
// location claim, and releases the bytes only when no space claims the
// digest afterward.

func (s *RPCSuite) TestBlobRelease_ReleasesClaimAndBytes() {
	t := s.T()
	service := s.ServiceID.DID()

	// Store a blob the usual way: allocate, upload, accept.
	data := testutil.RandomBytes(t, 64)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	size := uint64(len(data))
	space := testutil.RandomDID(t)

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

	// Remove the blob from the space.
	removeProof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), service, blob.Release.Command,
	))(t)
	remove := testutil.Must(blob.Release.Invoke(
		s.UploadServiceIdentity,
		service,
		&blob.ReleaseArguments{
			Space:  space,
			Digest: digest,
		},
		invocation.WithAudience(service),
		invocation.WithProofs(removeProof.Link()),
	))(t)
	assertReceiptOK(t, s.sendInvocationWithProofs(t, remove, removeProof))

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
	removeAgain := testutil.Must(blob.Release.Invoke(
		s.UploadServiceIdentity,
		service,
		&blob.ReleaseArguments{
			Space:  space,
			Digest: digest,
		},
		invocation.WithAudience(service),
		invocation.WithProofs(removeProof.Link()),
	))(t)
	assertReceiptOK(t, s.sendInvocationWithProofs(t, removeAgain, removeProof))
}

func (s *RPCSuite) TestBlobRelease_OtherSpaceRetainsBytes() {
	t := s.T()
	service := s.ServiceID.DID()

	data := testutil.RandomBytes(t, 64)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	size := uint64(len(data))
	spaceA := testutil.RandomDID(t)
	spaceB := testutil.RandomDID(t)

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
	removeProof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), service, blob.Release.Command,
	))(t)
	remove := testutil.Must(blob.Release.Invoke(
		s.UploadServiceIdentity,
		service,
		&blob.ReleaseArguments{
			Space:  spaceA,
			Digest: digest,
		},
		invocation.WithAudience(service),
		invocation.WithProofs(removeProof.Link()),
	))(t)
	assertReceiptOK(t, s.sendInvocationWithProofs(t, remove, removeProof))

	_, err := s.Acceptances.Get(t.Context(), digest, spaceA)
	require.ErrorIs(t, err, store.ErrNotFound, "spaceA's acceptance deleted")
	_, err = s.Acceptances.Get(t.Context(), digest, spaceB)
	require.NoError(t, err, "spaceB's acceptance retained")
	require.NotContains(t, s.Pieces.Removed(), digest,
		"spaceB still claims the digest — bytes retained")
}
