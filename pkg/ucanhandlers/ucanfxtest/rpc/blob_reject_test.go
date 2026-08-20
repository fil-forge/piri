package rpc_test

import (
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/errors/datamodel"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store"
)

// /blob/reject is performed by the upload service under a
// provider->upload-service delegation, mirroring /blob/remove: the subject is
// the storage provider and the space abandoning its upload travels in the
// arguments. It retires PARKED (never-accepted) blobs only.

func (s *RPCSuite) TestBlobReject_ParkedBlobReleased() {
	t := s.T()
	service := s.ServiceID.DID()

	// Allocate + upload, but never accept: the blob is parked.
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

	proof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), service, blob.Reject.Command,
	))(t)
	unalloc := testutil.Must(blob.Reject.Invoke(
		s.UploadServiceIdentity,
		service,
		&blob.RejectArguments{
			Space:  space,
			Digest: digest,
		},
		invocation.WithAudience(service),
		invocation.WithProofs(proof.Link()),
	))(t)
	assertReceiptOK(t, s.sendInvocationWithProofs(t, unalloc, proof))

	_, err := s.Allocations.Get(t.Context(), digest, space)
	require.ErrorIs(t, err, store.ErrNotFound, "allocation deleted")
	require.Contains(t, s.Pieces.Removed(), digest, "parked bytes released")

	// Idempotent.
	again := testutil.Must(blob.Reject.Invoke(
		s.UploadServiceIdentity,
		service,
		&blob.RejectArguments{
			Space:  space,
			Digest: digest,
		},
		invocation.WithAudience(service),
		invocation.WithProofs(proof.Link()),
	))(t)
	assertReceiptOK(t, s.sendInvocationWithProofs(t, again, proof))
}

func (s *RPCSuite) TestBlobReject_AcceptedBlobRefused() {
	t := s.T()
	service := s.ServiceID.DID()

	// Store a blob the usual way through accept — it now carries a claim.
	data := testutil.RandomBytes(t, 64)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	size := uint64(len(data))
	space := testutil.RandomDID(t)
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
	assertReceiptOK(t, s.sendInvocationWithProofs(t, accept, acceptProof))

	proof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), service, blob.Reject.Command,
	))(t)
	unalloc := testutil.Must(blob.Reject.Invoke(
		s.UploadServiceIdentity,
		service,
		&blob.RejectArguments{
			Space:  space,
			Digest: digest,
		},
		invocation.WithAudience(service),
		invocation.WithProofs(proof.Link()),
	))(t)
	rcpt := s.sendInvocationWithProofs(t, unalloc, proof)

	_, err := blob.Reject.Unpack(rcpt)
	var em datamodel.ErrorModel
	require.ErrorAs(t, err, &em)
	require.Equal(t, blob.BlobAcceptedErrorName, em.Name(),
		"accepted blobs are refused — release via /blob/remove")

	_, err = s.Acceptances.Get(t.Context(), digest, space)
	require.NoError(t, err, "acceptance untouched")
	require.NotContains(t, s.Pieces.Removed(), digest, "accepted bytes never touched")
}
