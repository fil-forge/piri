package blob

import (
	"bytes"
	"context"
	"fmt"
	"iter"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/libforge/commands/blob"
	httpcmds "github.com/fil-forge/libforge/commands/http"
	"github.com/fil-forge/libforge/digestutil"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/fil-forge/ucantone/validator"

	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

// PendingAllocations is the slice of allocationstore.AllocationStore the
// handlers depend on for allocations made without a digest: the pending
// records themselves, and the (digest, space) allocation a completed upload
// holds as its claim.
type PendingAllocations interface {
	GetPending(ctx context.Context, link cid.Cid) (allocation.Pending, error)
	PutPending(ctx context.Context, p allocation.Pending) error
	DeletePending(ctx context.Context, link cid.Cid) error
	ListPending(ctx context.Context) iter.Seq2[allocation.Pending, error]
	Get(ctx context.Context, digest multihash.Multihash, space did.DID) (allocation.Allocation, error)
	Put(ctx context.Context, alloc allocation.Allocation) error
	Delete(ctx context.Context, digest multihash.Multihash, space did.DID) error
}

// UploadDiscarder is the slice of the PDP piece-remover API that drops an
// upload which has not completed.
type UploadDiscarder interface {
	DiscardUpload(ctx context.Context, uploadID string) error
}

var (
	_ PendingAllocations = (allocationstore.AllocationStore)(nil)
	_ UploadDiscarder    = (pdptypes.PieceRemoverAPI)(nil)
)

// resolvePutDigest returns the digest the `/http/put` receipt reports for an
// accept that names only a digest code, with the pending allocation the data
// was uploaded to. Both the `/http/put` invocation and its receipt travel in
// the accept's container. The receipt must be the put task's, signed by the
// put's subject; the put must have been made to an allocation of this space
// and size; and the digest the node computed as it received the data must
// match the one the receipt reports, else the accept fails with
// BlobDigestMismatch.
func resolvePutDigest(
	ctx context.Context,
	pending PendingAllocations,
	meta ucan.Container,
	space did.DID,
	spec blob.BlobSpec,
	put promise.AwaitOK,
) (multihash.Multihash, allocation.Pending, error) {
	if meta == nil {
		return nil, allocation.Pending{}, fmt.Errorf("accept without a digest needs the %s invocation and receipt", httpcmds.Put.Command)
	}
	var putInv ucan.Invocation
	for _, inv := range meta.Invocations() {
		if inv.Task().Link() == put.Task {
			putInv = inv
			break
		}
	}
	if putInv == nil || putInv.Command() != httpcmds.Put.Command {
		return nil, allocation.Pending{}, fmt.Errorf("%s invocation %s not in the request", httpcmds.Put.Command, put.Task)
	}
	rcpt, ok := meta.Receipt(put.Task)
	if !ok {
		return nil, allocation.Pending{}, fmt.Errorf("%s receipt for %s not in the request", httpcmds.Put.Command, put.Task)
	}
	if rcpt.Issuer() != putInv.Subject() {
		return nil, allocation.Pending{}, fmt.Errorf("%s receipt is issued by %s, not the put's subject %s", httpcmds.Put.Command, rcpt.Issuer(), putInv.Subject())
	}
	// A receipt is an /ucan/assert/receipt invocation on the wire; verify its
	// signature as one. The issuer is the put's did:key, which the default
	// resolver handles.
	rcptInv, err := invocation.Decode(rcpt.Bytes())
	if err != nil {
		return nil, allocation.Pending{}, fmt.Errorf("decoding %s receipt: %w", httpcmds.Put.Command, err)
	}
	if err := validator.ValidateToken(ctx, rcptInv); err != nil {
		return nil, allocation.Pending{}, fmt.Errorf("verifying %s receipt: %w", httpcmds.Put.Command, err)
	}
	okBytes, _ := rcpt.Out().Unpack()
	if !rcpt.Out().IsOK() {
		return nil, allocation.Pending{}, fmt.Errorf("%s receipt reports a failure", httpcmds.Put.Command)
	}
	var putOK httpcmds.PutOK
	if err := putOK.UnmarshalCBOR(bytes.NewReader(okBytes)); err != nil {
		return nil, allocation.Pending{}, fmt.Errorf("decoding %s result: %w", httpcmds.Put.Command, err)
	}
	if putOK.Blob == nil || len(putOK.Blob.Digest) == 0 {
		return nil, allocation.Pending{}, fmt.Errorf("%s receipt reports no digest", httpcmds.Put.Command)
	}

	var putArgs httpcmds.PutArguments
	if err := putArgs.UnmarshalCBOR(bytes.NewReader(putInv.ArgumentsBytes())); err != nil {
		return nil, allocation.Pending{}, fmt.Errorf("decoding %s arguments: %w", httpcmds.Put.Command, err)
	}
	if _, hasDigest := putArgs.Body.Digest(); hasDigest ||
		putArgs.Body.DigestCode() != spec.DigestCode() || putArgs.Body.Size() != spec.Size() {
		return nil, allocation.Pending{}, fmt.Errorf("%s body does not match the accepted blob", httpcmds.Put.Command)
	}

	p, err := pending.GetPending(ctx, putArgs.Destination.Task)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, allocation.Pending{}, fmt.Errorf("no allocation %s for the put", putArgs.Destination.Task)
		}
		return nil, allocation.Pending{}, fmt.Errorf("getting pending allocation: %w", err)
	}
	if p.Space != space || p.Size != spec.Size() || p.DigestCode != spec.DigestCode() {
		return nil, allocation.Pending{}, fmt.Errorf("allocation %s is not for this blob", p.Allocation)
	}
	if len(p.Digest) == 0 {
		return nil, allocation.Pending{}, fmt.Errorf("blob for allocation %s has not been received", p.Allocation)
	}
	if !bytes.Equal(p.Digest, putOK.Blob.Digest) {
		return nil, allocation.Pending{}, errors.New(blob.BlobDigestMismatchErrorName,
			"received data hashes to %s, %s reports %s",
			digestutil.Format(p.Digest), httpcmds.Put.Command, digestutil.Format(putOK.Blob.Digest))
	}
	return p.Digest, p, nil
}

// releasePending drops an allocation made without a digest that was never
// accepted: its upload, its staged data, the pending record and, once the data
// was received, its claim on (digest, space). It returns the computed digest,
// or nil if the data was never received.
func releasePending(ctx context.Context, pending PendingAllocations, uploads UploadDiscarder, p allocation.Pending) (multihash.Multihash, error) {
	if err := uploads.DiscardUpload(ctx, p.UploadID); err != nil {
		return nil, fmt.Errorf("discarding upload: %w", err)
	}
	released, err := allocationstore.ReleasePending(ctx, pending, p)
	if err != nil {
		return nil, err
	}
	return released.Digest, nil
}

// putAllocation returns the allocation the `/http/put` task put was made to,
// when the put invocation travels in meta and its body is the blob digest. An
// upload service that sends only the task link leaves the allocation unknown.
func putAllocation(meta ucan.Container, put cid.Cid, digest multihash.Multihash) (cid.Cid, bool) {
	if meta == nil {
		return cid.Undef, false
	}
	for _, inv := range meta.Invocations() {
		if inv.Task().Link() != put || inv.Command() != httpcmds.Put.Command {
			continue
		}
		var args httpcmds.PutArguments
		if err := args.UnmarshalCBOR(bytes.NewReader(inv.ArgumentsBytes())); err != nil {
			return cid.Undef, false
		}
		if d, ok := args.Body.Digest(); !ok || !bytes.Equal(d, digest) {
			return cid.Undef, false
		}
		return args.Destination.Task, args.Destination.Task.Defined()
	}
	return cid.Undef, false
}

// currentAllocation makes link the space's allocation of digest, if a later
// allocation replaced it. A space without an allocation of digest is left
// alone.
func currentAllocation(ctx context.Context, allocs PendingAllocations, space did.DID, digest multihash.Multihash, link cid.Cid) error {
	alloc, err := allocs.Get(ctx, digest, space)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting allocation: %w", err)
	}
	if alloc.Allocation == link {
		return nil
	}
	alloc.Allocation = link
	if err := allocs.Put(ctx, alloc); err != nil {
		return fmt.Errorf("recording accepted allocation: %w", err)
	}
	return nil
}
