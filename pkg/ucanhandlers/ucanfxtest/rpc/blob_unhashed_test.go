package rpc_test

import (
	"testing"

	"github.com/fil-forge/libforge/commands/blob"
	httpcmds "github.com/fil-forge/libforge/commands/http"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/multikey/ed25519"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/fil-forge/ucantone/ucan/receipt"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

// An allocation can name only the hash function and the size. The node hashes
// the data as it is received, and /blob/accept checks that digest against the
// one the /http/put receipt reports. Both the put invocation and its receipt
// travel in the accept's container.

// unhashedBlob is a blob allocated without its digest, and the /http/put that
// uploads it.
type unhashedBlob struct {
	space  did.DID
	data   []byte
	digest multihash.Multihash
	cause  cid.Cid
	alloc  ucan.Invocation
	put    ucan.Invocation
	putter ucan.Issuer
}

func (b unhashedBlob) spec() blob.BlobSpec {
	return blob.SpecFromDigestCode(multihash.SHA2_256, uint64(len(b.data)))
}

// allocateUnhashed allocates space for random data without naming its digest,
// and builds the /http/put that would upload it.
func (s *RPCSuite) allocateUnhashed(t *testing.T, space did.DID, data []byte) unhashedBlob {
	t.Helper()
	b := unhashedBlob{
		space:  space,
		data:   data,
		digest: testutil.Must(multihash.Sum(data, multihash.SHA2_256, -1))(t),
		cause:  testutil.RandomCID(t),
	}
	var proof ucan.Delegation
	b.alloc, proof = s.newAllocate(t, &blob.AllocateArguments{
		Space: space,
		Blob:  b.spec(),
		Cause: b.cause,
	})
	ok := decodeAllocateOK(t, s.sendInvocationWithProofs(t, b.alloc, proof))
	require.Equal(t, uint64(len(data)), ok.Size, "the allocation reserves the full size")
	require.NotNil(t, ok.Address, "data without a digest is always uploaded")

	b.putter = testutil.Must(ed25519.GenerateIssuer())(t)
	b.put = testutil.Must(httpcmds.Put.Invoke(
		b.putter,
		b.putter.DID(),
		&httpcmds.PutArguments{
			Body:        b.spec(),
			Destination: promise.AwaitOK{Task: b.alloc.Task().Link()},
		},
		invocation.WithAudience(b.putter.DID()),
	))(t)
	return b
}

// upload stands in for the PDP service receiving the data: it records the
// digest on the pending allocation, claims (digest, space) unless another
// upload already does, and stores the bytes.
func (s *RPCSuite) upload(t *testing.T, b unhashedBlob) {
	t.Helper()
	ctx := t.Context()
	p, err := s.Allocations.GetPending(ctx, b.alloc.Task().Link())
	require.NoError(t, err)
	p.Digest = b.digest
	require.NoError(t, s.Allocations.PutPending(ctx, p))
	if _, err := s.Allocations.Get(ctx, b.digest, b.space); err != nil {
		require.ErrorIs(t, err, store.ErrNotFound)
		require.NoError(t, s.Allocations.Put(ctx, allocation.Allocation{
			Space:   b.space,
			Blob:    blob.Blob{Digest: b.digest, Size: uint64(len(b.data))},
			Expires: p.Expires,
			Cause:   p.Cause,
		}))
	}
	s.Pieces.Put(b.digest, b.data)
}

// putReceipt is the receipt the uploader issues for the put, reporting digest.
func putReceipt(t *testing.T, b unhashedBlob, issuer ucan.Issuer, digest multihash.Multihash) ucan.Receipt {
	t.Helper()
	return testutil.Must(receipt.IssueOK(issuer, b.put.Task().Link(), &httpcmds.PutOK{
		Blob: &httpcmds.PutBlob{Digest: digest},
	}))(t)
}

// accept sends a digest-less /blob/accept for b, with the given put
// invocations and receipts in its container.
func (s *RPCSuite) accept(t *testing.T, b unhashedBlob, invs []ucan.Invocation, rcpts []ucan.Receipt) (ucan.Invocation, ucan.Receipt) {
	t.Helper()
	service := s.ServiceID.DID()
	proof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), service, blob.Accept.Command,
	))(t)
	inv := testutil.Must(blob.Accept.Invoke(
		s.UploadServiceIdentity,
		service,
		&blob.AcceptArguments{
			Space: b.space,
			Blob:  b.spec(),
			Put:   promise.AwaitOK{Task: b.put.Task().Link()},
		},
		invocation.WithAudience(service),
		invocation.WithProofs(proof.Link()),
	))(t)
	opts := []execution.RequestOption{execution.WithDelegations(proof)}
	if len(invs) > 0 {
		opts = append(opts, execution.WithInvocations(invs...))
	}
	if len(rcpts) > 0 {
		opts = append(opts, execution.WithReceipts(rcpts...))
	}
	resp, err := s.RPCClient(t).Execute(execution.NewRequest(t.Context(), inv, opts...))
	require.NoError(t, err)
	return inv, resp.Receipt()
}

func (s *RPCSuite) rejectAllocation(t *testing.T, space did.DID, link cid.Cid) ucan.Receipt {
	t.Helper()
	service := s.ServiceID.DID()
	proof := testutil.Must(delegation.Delegate(
		s.ServiceID, s.UploadServiceIdentity.DID(), service, blob.Reject.Command,
	))(t)
	inv := testutil.Must(blob.Reject.Invoke(
		s.UploadServiceIdentity,
		service,
		ptr(blob.RejectByAllocation(space, link)),
		invocation.WithAudience(service),
		invocation.WithProofs(proof.Link()),
	))(t)
	return s.sendInvocationWithProofs(t, inv, proof)
}

func (s *RPCSuite) TestBlobUnhashed_AllocateUploadAccept() {
	t := s.T()
	b := s.allocateUnhashed(t, testutil.RandomDID(t), testutil.RandomBytes(t, 64))

	p, err := s.Allocations.GetPending(t.Context(), b.alloc.Task().Link())
	require.NoError(t, err, "the allocation is recorded as pending")
	require.Equal(t, b.space, p.Space)
	require.Equal(t, uint64(len(b.data)), p.Size)
	require.Equal(t, uint64(multihash.SHA2_256), p.DigestCode)
	require.Equal(t, b.cause, p.Cause)
	require.Empty(t, p.Digest)

	s.upload(t, b)
	inv, rcpt := s.accept(t, b,
		[]ucan.Invocation{b.put},
		[]ucan.Receipt{putReceipt(t, b, b.putter, b.digest)})
	ok := decodeAcceptOK(t, rcpt)

	acc, err := s.Acceptances.Get(t.Context(), b.digest, b.space)
	require.NoError(t, err, "the acceptance is keyed by the digest the node computed")
	require.Equal(t, inv.Task().Link(), acc.Cause)
	require.Equal(t, ok.Site, acc.Site)

	p, err = s.Allocations.GetPending(t.Context(), b.alloc.Task().Link())
	require.NoError(t, err)
	require.True(t, p.Accepted, "the pending allocation is marked accepted")

	assertReceiptFailure(t, s.rejectAllocation(t, b.space, b.alloc.Task().Link()), blob.BlobAcceptedErrorName)
}

func (s *RPCSuite) TestBlobUnhashed_UnsupportedDigestCode() {
	t := s.T()
	inv, proof := s.newAllocate(t, &blob.AllocateArguments{
		Space: testutil.RandomDID(t),
		Blob:  blob.SpecFromDigestCode(multihash.BLAKE2B_MIN+31, 64),
		Cause: testutil.RandomCID(t),
	})
	assertReceiptFailure(t, s.sendInvocationWithProofs(t, inv, proof), blob.UnsupportedDigestCodeErrorName)
}

func (s *RPCSuite) TestBlobUnhashed_DigestMismatch() {
	t := s.T()
	b := s.allocateUnhashed(t, testutil.RandomDID(t), testutil.RandomBytes(t, 64))
	s.upload(t, b)

	wrong := testutil.Must(multihash.Sum(testutil.RandomBytes(t, 64), multihash.SHA2_256, -1))(t)
	_, rcpt := s.accept(t, b,
		[]ucan.Invocation{b.put},
		[]ucan.Receipt{putReceipt(t, b, b.putter, wrong)})
	assertReceiptFailure(t, rcpt, blob.BlobDigestMismatchErrorName)

	_, err := s.Acceptances.Get(t.Context(), b.digest, b.space)
	require.ErrorIs(t, err, store.ErrNotFound, "nothing is accepted")
}

func (s *RPCSuite) TestBlobUnhashed_AcceptNeedsPutAndReceipt() {
	t := s.T()
	b := s.allocateUnhashed(t, testutil.RandomDID(t), testutil.RandomBytes(t, 64))
	s.upload(t, b)
	stranger := testutil.Must(ed25519.GenerateIssuer())(t)

	cases := map[string]struct {
		invs  []ucan.Invocation
		rcpts []ucan.Receipt
	}{
		"no put":             {rcpts: []ucan.Receipt{putReceipt(t, b, b.putter, b.digest)}},
		"no receipt":         {invs: []ucan.Invocation{b.put}},
		"receipt by another": {invs: []ucan.Invocation{b.put}, rcpts: []ucan.Receipt{putReceipt(t, b, stranger, b.digest)}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, rcpt := s.accept(t, b, c.invs, c.rcpts)
			require.True(t, rcpt.Out().IsErr(), "accept fails")
			_, err := s.Acceptances.Get(t.Context(), b.digest, b.space)
			require.ErrorIs(t, err, store.ErrNotFound, "nothing is accepted")
		})
	}
}

func (s *RPCSuite) TestBlobUnhashed_AcceptBeforeUpload() {
	t := s.T()
	b := s.allocateUnhashed(t, testutil.RandomDID(t), testutil.RandomBytes(t, 64))
	_, rcpt := s.accept(t, b,
		[]ucan.Invocation{b.put},
		[]ucan.Receipt{putReceipt(t, b, b.putter, b.digest)})
	require.True(t, rcpt.Out().IsErr(), "nothing has been received to accept")
}

func (s *RPCSuite) TestBlobUnhashed_RejectBeforeUpload() {
	t := s.T()
	b := s.allocateUnhashed(t, testutil.RandomDID(t), testutil.RandomBytes(t, 64))
	p, err := s.Allocations.GetPending(t.Context(), b.alloc.Task().Link())
	require.NoError(t, err)

	assertReceiptOK(t, s.rejectAllocation(t, b.space, b.alloc.Task().Link()))
	require.Contains(t, s.Pieces.Discarded(), p.UploadID, "the upload is discarded")
	_, err = s.Allocations.GetPending(t.Context(), b.alloc.Task().Link())
	require.ErrorIs(t, err, store.ErrNotFound, "the pending allocation is deleted")

	assertReceiptOK(t, s.rejectAllocation(t, b.space, b.alloc.Task().Link()))
}

func (s *RPCSuite) TestBlobUnhashed_RejectOtherSpace() {
	t := s.T()
	b := s.allocateUnhashed(t, testutil.RandomDID(t), testutil.RandomBytes(t, 64))

	assertReceiptOK(t, s.rejectAllocation(t, testutil.RandomDID(t), b.alloc.Task().Link()))
	_, err := s.Allocations.GetPending(t.Context(), b.alloc.Task().Link())
	require.NoError(t, err, "another space cannot reject the allocation")
}

func (s *RPCSuite) TestBlobUnhashed_RejectAfterUpload() {
	t := s.T()
	b := s.allocateUnhashed(t, testutil.RandomDID(t), testutil.RandomBytes(t, 64))
	s.upload(t, b)

	assertReceiptOK(t, s.rejectAllocation(t, b.space, b.alloc.Task().Link()))
	_, err := s.Allocations.Get(t.Context(), b.digest, b.space)
	require.ErrorIs(t, err, store.ErrNotFound, "the upload's claim is released")
	require.Contains(t, s.Pieces.Removed(), b.digest, "unclaimed bytes are released")
}

// Two uploads of the same content into the same space share one (digest,
// space) allocation, held by whichever arrived first. Rejecting that one hands
// the claim to the other, which then accepts as normal.
func (s *RPCSuite) TestBlobUnhashed_RejectSharedContent() {
	t := s.T()
	space := testutil.RandomDID(t)
	data := testutil.RandomBytes(t, 64)
	first := s.allocateUnhashed(t, space, data)
	second := s.allocateUnhashed(t, space, data)
	s.upload(t, first)
	s.upload(t, second)

	alloc, err := s.Allocations.Get(t.Context(), first.digest, space)
	require.NoError(t, err)
	require.Equal(t, first.cause, alloc.Cause, "the first upload holds the claim")

	assertReceiptOK(t, s.rejectAllocation(t, space, first.alloc.Task().Link()))

	alloc, err = s.Allocations.Get(t.Context(), first.digest, space)
	require.NoError(t, err, "the claim survives")
	require.Equal(t, second.cause, alloc.Cause, "the claim passes to the second upload")
	require.NotContains(t, s.Pieces.Removed(), first.digest, "claimed bytes are kept")

	_, rcpt := s.accept(t, second,
		[]ucan.Invocation{second.put},
		[]ucan.Receipt{putReceipt(t, second, second.putter, second.digest)})
	decodeAcceptOK(t, rcpt)
}

// Rejecting an upload that does not hold the (digest, space) claim leaves the
// claim with the upload that does.
func (s *RPCSuite) TestBlobUnhashed_RejectNonHolder() {
	t := s.T()
	space := testutil.RandomDID(t)
	data := testutil.RandomBytes(t, 64)
	first := s.allocateUnhashed(t, space, data)
	second := s.allocateUnhashed(t, space, data)
	s.upload(t, first)
	s.upload(t, second)

	assertReceiptOK(t, s.rejectAllocation(t, space, second.alloc.Task().Link()))

	alloc, err := s.Allocations.Get(t.Context(), first.digest, space)
	require.NoError(t, err)
	require.Equal(t, first.cause, alloc.Cause, "the holder keeps its claim")
	require.NotContains(t, s.Pieces.Removed(), first.digest)
}
