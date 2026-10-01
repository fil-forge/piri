package rpc_test

import (
	"testing"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/errors/datamodel"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/ipfs/go-cid"
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

	// Allocate + upload, but never accept: the blob is parked.
	data := testutil.RandomBytes(t, 64)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	size := uint64(len(data))
	space := testutil.RandomDID(t)

	alloc, allocProof := s.newAllocate(t, &blob.AllocateArguments{
		Space: space,
		Blob:  blob.SpecFromBlob(blob.Blob{Digest: digest, Size: size}),
		Cause: testutil.RandomCID(t),
	})
	assertReceiptOK(t, s.sendInvocationWithProofs(t, alloc, allocProof))
	s.Pieces.Put(digest, data)

	assertReceiptOK(t, s.rejectAllocation(t, alloc.Task().Link()))

	_, err := s.Allocations.Get(t.Context(), digest, space)
	require.ErrorIs(t, err, store.ErrNotFound, "allocation deleted")
	require.Contains(t, s.Pieces.Removed(), digest, "parked bytes released")

	assertReceiptOK(t, s.rejectAllocation(t, alloc.Task().Link())) // idempotent
}

func (s *RPCSuite) TestBlobReject_AcceptedBlobRefused() {
	t := s.T()
	service := s.ServiceID.DID()

	// Store a blob the usual way through accept — it now carries a claim.
	data := testutil.RandomBytes(t, 64)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	size := uint64(len(data))
	space := testutil.RandomDID(t)
	alloc, allocProof := s.newAllocate(t, &blob.AllocateArguments{
		Space: space,
		Blob:  blob.SpecFromBlob(blob.Blob{Digest: digest, Size: size}),
		Cause: testutil.RandomCID(t),
	})
	assertReceiptOK(t, s.sendInvocationWithProofs(t, alloc, allocProof))
	s.Pieces.Put(digest, data)

	acceptProof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), service, blob.Accept.Command,
	))(t)
	accept := testutil.Must(blob.Accept.Invoke(
		s.UploadServiceIdentity,
		service,
		&blob.AcceptArguments{
			Space: space,
			Blob:  blob.SpecFromBlob(blob.Blob{Digest: digest, Size: size}),
			Put:   promise.AwaitOK{Task: testutil.RandomCID(t)},
		},
		invocation.WithAudience(service),
		invocation.WithProofs(acceptProof.Link()),
	))(t)
	assertReceiptOK(t, s.sendInvocationWithProofs(t, accept, acceptProof))

	rcpt := s.rejectAllocation(t, alloc.Task().Link())

	_, err := blob.Reject.Unpack(rcpt)
	var em datamodel.ErrorModel
	require.ErrorAs(t, err, &em)
	require.Equal(t, blob.BlobAcceptedErrorName, em.Name(),
		"accepted blobs are refused — release via /blob/remove")

	_, err = s.Acceptances.Get(t.Context(), digest, space)
	require.NoError(t, err, "acceptance untouched")
	require.NotContains(t, s.Pieces.Removed(), digest, "accepted bytes never touched")
}

// A later allocation of the same blob in the same space replaces the earlier
// one: rejecting the earlier allocation leaves the later one in place, and
// rejecting the later one releases the blob.
func (s *RPCSuite) TestBlobReject_ReplacedAllocationIsLeftAlone() {
	t := s.T()
	data := testutil.RandomBytes(t, 64)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	space := testutil.RandomDID(t)
	allocate := func() ucan.Invocation {
		inv, proof := s.newAllocate(t, &blob.AllocateArguments{
			Space: space,
			Blob:  blob.SpecFromBlob(blob.Blob{Digest: digest, Size: uint64(len(data))}),
			Cause: testutil.RandomCID(t),
		})
		assertReceiptOK(t, s.sendInvocationWithProofs(t, inv, proof))
		return inv
	}
	first, second := allocate(), allocate()
	s.Pieces.Put(digest, data)

	assertReceiptOK(t, s.rejectAllocation(t, first.Task().Link()))
	alloc, err := s.Allocations.Get(t.Context(), digest, space)
	require.NoError(t, err, "the later allocation stays")
	require.Equal(t, second.Task().Link(), alloc.Allocation)
	require.NotContains(t, s.Pieces.Removed(), digest)

	assertReceiptOK(t, s.rejectAllocation(t, second.Task().Link()))
	_, err = s.Allocations.Get(t.Context(), digest, space)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Contains(t, s.Pieces.Removed(), digest)
}

// rejectAllocation sends a /blob/reject of the allocation the allocate task
// link made, issued by the upload service as in production.
func (s *RPCSuite) rejectAllocation(t *testing.T, link cid.Cid) ucan.Receipt {
	t.Helper()
	service := s.ServiceID.DID()
	proof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), service, blob.Reject.Command,
	))(t)
	inv := testutil.Must(blob.Reject.Invoke(
		s.UploadServiceIdentity,
		service,
		&blob.RejectArguments{Allocation: link},
		invocation.WithAudience(service),
		invocation.WithProofs(proof.Link()),
	))(t)
	return s.sendInvocationWithProofs(t, inv, proof)
}
