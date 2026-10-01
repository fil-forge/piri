package rpc_test

import (
	"testing"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/server/middleware"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	blobhandler "github.com/fil-forge/piri/pkg/ucanhandlers/blob"
)

// /blob/allocate is provider-scoped: the invocation subject is the storage
// provider and the space being allocated into travels in
// AllocateArguments.Space. The upload service issues it against a delegation
// rooted at the provider — see newAllocate — since the route refuses a
// self-signed invocation. Allocations are keyed on (digest, space).

func (s *RPCSuite) TestBlobAllocate_Basic() {
	t := s.T()
	digest := testutil.RandomMultihash(t)
	size := uint64(123)
	cause := testutil.RandomCID(t)
	space := testutil.RandomDID(t)

	inv, proof := s.newAllocate(t, &blob.AllocateArguments{
		Space: space,
		Blob:  blob.SpecFromDigest(digest, size),
		Cause: cause,
	})

	rcpt := s.sendInvocationWithProofs(t, inv, proof)
	ok := decodeAllocateOK(t, rcpt)

	require.Equal(t, size, ok.Size, "size to upload should match request")
	require.NotNil(t, ok.Address, "address required when blob not yet stored")

	stored, err := s.Allocations.Get(t.Context(), digest, space)
	require.NoError(t, err, "allocation persisted in store")
	require.Equal(t, digest, stored.Blob.Digest)
	require.Equal(t, size, stored.Blob.Size)
	require.Equal(t, space, stored.Space)
	require.Equal(t, cause, stored.Cause, "cause records the args.Cause CID the client supplied")
}

// TestBlobAllocate_SizeLimitExceeded is the end-to-end shape of the fix: an
// oversized allocation must come back as a receipt failure the upload service
// can interpret, not a transport error.
//
// Note the invocation reaches the handler at all. blob.Allocate is a bare
// binding.Bind with no policy attached, unlike the older capability which
// carried policy.LessThanOrEqual(".blob.size", ...) and would have rejected
// this at the validator. The handler check is the only guard.
func (s *RPCSuite) TestBlobAllocate_SizeLimitExceeded() {
	t := s.T()
	digest := testutil.RandomMultihash(t)
	space := testutil.RandomDID(t)

	// One byte past the default cap. Costs nothing to run: only the declared
	// size is oversized, no bytes are materialized.
	const overLimit = 266338304 + 1

	inv, proof := s.newAllocate(t, &blob.AllocateArguments{
		Space: space,
		Blob:  blob.SpecFromDigest(digest, overLimit),
		Cause: testutil.RandomCID(t),
	})

	rcpt := s.sendInvocationWithProofs(t, inv, proof)
	assertReceiptFailure(t, rcpt, blobhandler.BlobSizeLimitExceededErrorName)

	_, err := s.Allocations.Get(t.Context(), digest, space)
	require.Error(t, err, "no allocation persisted for a rejected blob")
}

func (s *RPCSuite) TestBlobAllocate_RepeatSameBlob() {
	t := s.T()

	// Use a deterministic digest derived from real bytes so the bytes we
	// later seed via Pieces.Put match.
	data := testutil.RandomBytes(t, 64)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	size := uint64(len(data))
	cause := testutil.RandomCID(t)
	space := testutil.RandomDID(t)

	allocate := func() *blob.AllocateOK {
		inv, proof := s.newAllocate(t, &blob.AllocateArguments{
			Space: space,
			Blob:  blob.SpecFromDigest(digest, size),
			Cause: cause,
		})
		return decodeAllocateOK(t, s.sendInvocationWithProofs(t, inv, proof))
	}

	// First allocation: blob has never been seen, so the handler reserves
	// space and hands back an upload URL.
	first := allocate()
	require.Equal(t, size, first.Size, "first allocation reserves the requested size")
	require.NotNil(t, first.Address, "first allocation returns an upload URL")

	// Second allocation, same blob, same space, no upload in between: the
	// allocation already exists in store, so size to upload is 0 — but
	// the blob still hasn't arrived, so an upload URL is still offered.
	second := allocate()
	require.Equal(t, uint64(0), second.Size, "re-allocate before upload returns Size=0 (already reserved)")
	require.NotNil(t, second.Address, "upload still pending so Address remains")

	// Simulate the upload landing.
	s.Pieces.Put(digest, data)

	// Third allocation: the data is now in the piece store. Size stays
	// 0 (already allocated) and Address is dropped (no upload needed).
	third := allocate()
	require.Equal(t, uint64(0), third.Size, "post-upload re-allocate returns Size=0")
	require.Nil(t, third.Address, "post-upload re-allocate omits Address — nothing to upload")
}

// TestBlobAllocate_RejectsSelfSigned covers the checks the RPC routes are
// served behind. A self-signed invocation needs no proofs and claims the
// subject's whole authority, so the node refuses one; and an invocation
// subjected to anyone but this node is not ours to answer.
func (s *RPCSuite) TestBlobAllocate_RejectsSelfSigned() {
	t := s.T()
	args := &blob.AllocateArguments{
		Space: testutil.RandomDID(t),
		Blob:  blob.SpecFromDigest(testutil.RandomMultihash(t), 123),
		Cause: testutil.RandomCID(t),
	}

	t.Run("issued by its own subject", func(t *testing.T) {
		stranger := testutil.RandomIssuer(t)
		inv := testutil.Must(blob.Allocate.Invoke(
			stranger,
			stranger.DID(),
			args,
			invocation.WithAudience(s.ServiceID.DID()),
		))(t)
		assertReceiptFailure(t, s.sendInvocation(t, inv), middleware.SelfSignedInvocationErrorName)
	})

	t.Run("subjected to another node", func(t *testing.T) {
		other := testutil.RandomIssuer(t)
		proof := testutil.Must(delegation.Delegate(
			other, s.UploadServiceIdentity.DID(), other.DID(), blob.Allocate.Command,
		))(t)
		inv := testutil.Must(blob.Allocate.Invoke(
			s.UploadServiceIdentity,
			other.DID(),
			args,
			invocation.WithAudience(s.ServiceID.DID()),
			invocation.WithProofs(proof.Link()),
		))(t)
		assertReceiptFailure(t, s.sendInvocationWithProofs(t, inv, proof), middleware.InvalidSubjectErrorName)
	})
}
