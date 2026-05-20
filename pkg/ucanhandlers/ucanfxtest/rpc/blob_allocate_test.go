package rpc_test

import (
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

// /blob/allocate is service-scoped: the invocation must be signed by the
// service (which is also the subject) per the RequireSubject check in
// pkg/ucanhandlers/blob/allocate.go:79. AllocateArguments has no Space
// field — the handler reads space from the invocation subject. So
// allocations are effectively keyed under the service DID until the
// handler grows multi-space support.

func (s *RPCSuite) TestBlobAllocate_Basic() {
	t := s.T()
	digest := testutil.RandomMultihash(t)
	size := uint64(123)
	cause := testutil.RandomCID(t)

	inv := testutil.Must(blob.Allocate.Invoke(
		s.ServiceID,
		s.ServiceID.DID(),
		&blob.AllocateArguments{
			Blob:  blob.Blob{Digest: digest, Size: size},
			Cause: cause,
		},
		invocation.WithAudience(s.ServiceID.DID()),
	))(t)

	rcpt := s.sendInvocation(t, inv)
	ok := decodeAllocateOK(t, rcpt)

	require.Equal(t, size, ok.Size, "size to upload should match request")
	require.NotNil(t, ok.Address, "address required when blob not yet stored")

	stored, err := s.Allocations.Get(t.Context(), digest, s.ServiceID.DID())
	require.NoError(t, err, "allocation persisted in store")
	require.Equal(t, digest, stored.Blob.Digest)
	require.Equal(t, size, stored.Blob.Size)
	require.Equal(t, s.ServiceID.DID(), stored.Space)
	require.Equal(t, cause, stored.Cause, "cause records the args.Cause CID the client supplied")
}

func (s *RPCSuite) TestBlobAllocate_RepeatSameBlob() {
	t := s.T()

	// Use a deterministic digest derived from real bytes so the bytes we
	// later seed via Pieces.Put match.
	data := testutil.RandomBytes(t, 64)
	digest := testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t)
	size := uint64(len(data))
	cause := testutil.RandomCID(t)

	allocate := func() *blob.AllocateOK {
		inv := testutil.Must(blob.Allocate.Invoke(
			s.ServiceID,
			s.ServiceID.DID(),
			&blob.AllocateArguments{
				Blob:  blob.Blob{Digest: digest, Size: size},
				Cause: cause,
			},
			invocation.WithAudience(s.ServiceID.DID()),
		))(t)
		return decodeAllocateOK(t, s.sendInvocation(t, inv))
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
