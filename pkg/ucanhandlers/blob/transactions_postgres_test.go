package blob

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fil-forge/libforge/commands"
	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/libforge/testutil"
	ucanerrors "github.com/fil-forge/ucantone/errors"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/command"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/internal/testutil/pdpfake"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
)

// failingRemover fails every request to release a blob's bytes, the last
// write of a release or a reject.
type failingRemover struct{}

func (failingRemover) RemovePiece(context.Context, multihash.Multihash) error {
	return errors.New("removal queue unavailable")
}

type postgresBlob struct {
	db      *harmonydb.DB
	tx      store.Transactor
	accepts *acceptancestore.PostgresStore
	allocs  *allocationstore.PostgresStore
	claims  *invocationstore.PostgresStore
}

func newPostgresBlob(t *testing.T) postgresBlob {
	t.Helper()
	db := piritestutil.NewHarmonyDB(t)
	return postgresBlob{
		db:      db,
		tx:      harmonytx.New(db),
		accepts: acceptancestore.NewPostgresStore(db),
		allocs:  allocationstore.NewPostgresStore(db),
		claims:  invocationstore.NewPostgresStore(db),
	}
}

// allocated puts an allocation of a random blob in a random space.
func (p postgresBlob) allocated(t *testing.T) allocation.Allocation {
	t.Helper()
	alloc := allocation.Allocation{
		Space:      testutil.RandomDID(t),
		Blob:       blob.Blob{Digest: testutil.RandomMultihash(t), Size: 4},
		Expires:    1,
		Cause:      testutil.RandomCID(t),
		Allocation: testutil.RandomCID(t),
	}
	require.NoError(t, p.allocs.Put(t.Context(), alloc))
	return alloc
}

func (p postgresBlob) acceptanceOf(t *testing.T, alloc allocation.Allocation, site cid.Cid) acceptance.Acceptance {
	t.Helper()
	return acceptance.Acceptance{
		Space:      alloc.Space,
		Blob:       acceptance.Blob{Digest: alloc.Blob.Digest, Size: alloc.Blob.Size},
		PDPAccept:  promise.AwaitOK{Task: testutil.RandomCID(t)},
		Cause:      testutil.RandomCID(t),
		Site:       site,
		Allocation: &alloc.Allocation,
	}
}

// A release whose last write fails leaves the claim, the acceptance and the
// allocation as they were.
func TestRelease_FailureLeavesRecords(t *testing.T) {
	p := newPostgresBlob(t)
	ctx := t.Context()
	alloc := p.allocated(t)
	claim := testutil.Must(invocation.Invoke(testutil.WebService, testutil.WebService.DID(), command.New("/assert/location"), &commands.Unit{}))(t)
	require.NoError(t, p.claims.Put(ctx, claim))
	require.NoError(t, p.accepts.Put(ctx, p.acceptanceOf(t, alloc, claim.Link())))

	err := Release(ctx, ReleaseDeps{
		Allocations: p.allocs,
		Acceptances: p.accepts,
		ClaimStore:  p.claims,
		Adverts:     pdpfake.NewAdvertQueue(),
		Pieces:      failingRemover{},
		Tx:          p.tx,
	}, &ReleaseRequest{Space: alloc.Space, Digest: alloc.Blob.Digest})
	require.ErrorContains(t, err, "removal queue unavailable")

	_, err = p.claims.Get(ctx, claim.Link())
	require.NoError(t, err, "the claim stays")
	_, err = p.accepts.Get(ctx, alloc.Blob.Digest, alloc.Space)
	require.NoError(t, err, "the acceptance stays")
	_, err = p.allocs.Get(ctx, alloc.Blob.Digest, alloc.Space)
	require.NoError(t, err, "the allocation stays")
}

// A reject whose last write fails leaves the allocation in place.
func TestReject_FailureLeavesAllocation(t *testing.T) {
	p := newPostgresBlob(t)
	ctx := t.Context()
	alloc := p.allocated(t)

	err := RejectAllocation(ctx, RejectDeps{
		ID:          identity.Identity{Issuer: testutil.WebService},
		Allocations: p.allocs,
		Acceptances: p.accepts,
		Pieces:      failingRemover{},
		Pending:     p.allocs,
		Uploads:     pdpfake.NewPieces(),
		Tx:          p.tx,
	}, alloc.Allocation)
	require.ErrorContains(t, err, "removal queue unavailable")

	got, err := p.allocs.Get(ctx, alloc.Blob.Digest, alloc.Space)
	require.NoError(t, err, "the allocation is rolled back into place")
	require.Equal(t, alloc.Allocation, got.Allocation)
}

// A reject of an allocation whose accept holds the allocation's lock waits
// for the accept, then sees its acceptance and refuses.
func TestReject_WaitsForAccept(t *testing.T) {
	p := newPostgresBlob(t)
	ctx := t.Context()
	alloc := p.allocated(t)

	locked, finish := make(chan struct{}), make(chan struct{})
	accepted := make(chan error, 1)
	go func() {
		accepted <- p.tx.Run(ctx, func(ctx context.Context) error {
			if err := p.allocs.Lock(ctx, alloc.Blob.Digest, alloc.Space); err != nil {
				return err
			}
			if err := p.accepts.Put(ctx, p.acceptanceOf(t, alloc, testutil.RandomCID(t))); err != nil {
				return err
			}
			close(locked)
			<-finish
			return nil
		})
	}()
	<-locked

	rejected := make(chan error, 1)
	go func() {
		rejected <- RejectAllocation(ctx, RejectDeps{
			ID:          identity.Identity{Issuer: testutil.WebService},
			Allocations: p.allocs,
			Acceptances: p.accepts,
			Pieces:      pdpfake.NewPieces(),
			Pending:     p.allocs,
			Uploads:     pdpfake.NewPieces(),
			Tx:          p.tx,
		}, alloc.Allocation)
	}()
	select {
	case err := <-rejected:
		t.Fatalf("the reject finished while the accept held the allocation: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(finish)
	require.NoError(t, <-accepted)
	err := <-rejected
	var named ucanerrors.Named
	require.True(t, errors.As(err, &named), "want a named failure, got %v", err)
	require.Equal(t, blob.BlobAcceptedErrorName, named.Name())
	_, err = p.allocs.Get(ctx, alloc.Blob.Digest, alloc.Space)
	require.NoError(t, err, "the accepted allocation stays")
}

// rejectFirst runs drop in a transaction that holds its locks until the
// returned commit is called, so an accept started meanwhile waits for it.
func (p postgresBlob) rejectFirst(t *testing.T, drop func(ctx context.Context) error) (commit func()) {
	t.Helper()
	held, finish := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- p.tx.Run(t.Context(), func(ctx context.Context) error {
			if err := drop(ctx); err != nil {
				return err
			}
			close(held)
			<-finish
			return nil
		})
	}()
	<-held
	return func() {
		close(finish)
		require.NoError(t, <-done)
	}
}

func (p postgresBlob) acceptDeps() AcceptDeps {
	return AcceptDeps{
		ID:          identity.Identity{Issuer: testutil.WebService},
		Acceptances: p.accepts,
		Pieces:      pdpfake.NewPieces(),
		Commp:       pdpfake.NewCommp(),
		ClaimStore:  p.claims,
		Publisher:   publishFunc(func(context.Context, ucan.Invocation) error { return nil }),
		Pending:     p.allocs,
		Tx:          p.tx,
	}
}

// acceptAfter starts an accept of req, checks it waits while the reject holds
// its locks, then commits the reject and returns the accept's error.
func (p postgresBlob) acceptAfter(t *testing.T, deps AcceptDeps, req *AcceptRequest, commit func()) error {
	t.Helper()
	accepted := make(chan error, 1)
	go func() {
		_, err := Accept(t.Context(), deps, req)
		accepted <- err
	}()
	select {
	case err := <-accepted:
		t.Fatalf("the accept finished while the reject held the allocation: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	commit()
	return <-accepted
}

// An accept of an allocation a reject released first is refused, and leaves
// no acceptance.
func TestAccept_AfterRejectIsRefused(t *testing.T) {
	p := newPostgresBlob(t)
	ctx := t.Context()
	alloc := p.allocated(t)
	deps := p.acceptDeps()
	deps.Pieces.(*pdpfake.Pieces).Put(alloc.Blob.Digest, []byte("data"))

	commit := p.rejectFirst(t, func(ctx context.Context) error {
		if err := p.allocs.Lock(ctx, alloc.Blob.Digest, alloc.Space); err != nil {
			return err
		}
		_, err := p.allocs.ReleaseByTask(ctx, alloc.Allocation)
		return err
	})
	err := p.acceptAfter(t, deps, &AcceptRequest{
		Space:      alloc.Space,
		Blob:       alloc.Blob,
		Cause:      testutil.RandomCID(t),
		Allocation: &alloc.Allocation,
	}, commit)
	require.ErrorContains(t, err, "was released before it was accepted")

	_, err = p.accepts.Get(ctx, alloc.Blob.Digest, alloc.Space)
	require.ErrorIs(t, err, store.ErrNotFound, "no acceptance is recorded")
}

// The same for an allocation made without a digest: the reject releases its
// pending record and claim first, and the accept is refused.
func TestAccept_WithoutDigestAfterRejectIsRefused(t *testing.T) {
	p := newPostgresBlob(t)
	ctx := t.Context()
	pending := allocation.Pending{
		Allocation: testutil.RandomCID(t),
		Space:      testutil.RandomDID(t),
		Size:       4,
		DigestCode: multihash.SHA2_256,
		Cause:      testutil.RandomCID(t),
		Expires:    1,
		UploadID:   "upload",
		Digest:     testutil.RandomMultihash(t),
	}
	require.NoError(t, p.allocs.PutPending(ctx, pending))
	_, err := p.allocs.Claim(ctx, allocation.Allocation{
		Space:      pending.Space,
		Blob:       blob.Blob{Digest: pending.Digest, Size: pending.Size},
		Cause:      pending.Cause,
		Allocation: pending.Allocation,
	})
	require.NoError(t, err)
	deps := p.acceptDeps()
	deps.Pieces.(*pdpfake.Pieces).Put(pending.Digest, []byte("data"))

	commit := p.rejectFirst(t, func(ctx context.Context) error {
		if err := p.allocs.LockPending(ctx, pending.Allocation); err != nil {
			return err
		}
		_, err := p.allocs.ReleasePending(ctx, pending)
		return err
	})
	err = p.acceptAfter(t, deps, &AcceptRequest{
		Space:         pending.Space,
		Blob:          blob.Blob{Digest: pending.Digest, Size: pending.Size},
		Cause:         testutil.RandomCID(t),
		Allocation:    &pending.Allocation,
		WithoutDigest: true,
	}, commit)
	require.ErrorContains(t, err, "was released before it was accepted")

	_, err = p.accepts.Get(ctx, pending.Digest, pending.Space)
	require.ErrorIs(t, err, store.ErrNotFound, "no acceptance is recorded")
}

// With no allocation of the blob in the space, an accept that names no
// allocation still waits for a release that holds the blob's lock.
func TestAccept_WaitsForReleaseWithoutAllocation(t *testing.T) {
	p := newPostgresBlob(t)
	ctx := t.Context()
	space, digest := testutil.RandomDID(t), testutil.RandomMultihash(t)
	deps := p.acceptDeps()
	deps.Pieces.(*pdpfake.Pieces).Put(digest, []byte("data"))

	commit := p.rejectFirst(t, func(ctx context.Context) error {
		return p.allocs.Lock(ctx, digest, space)
	})
	err := p.acceptAfter(t, deps, &AcceptRequest{
		Space: space,
		Blob:  blob.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}, commit)
	require.NoError(t, err)

	_, err = p.accepts.Get(ctx, digest, space)
	require.NoError(t, err)
}

// blockingAllocator holds the first AllocatePiece until release is closed,
// and counts the calls that reach it.
type blockingAllocator struct {
	*pdpfake.Pieces
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (b *blockingAllocator) AllocatePiece(ctx context.Context, alloc types.PieceAllocation) (*types.AllocatedPiece, error) {
	if b.calls.Add(1) == 1 {
		close(b.entered)
		<-b.release
	}
	return b.Pieces.AllocatePiece(ctx, alloc)
}

// A retry of a digest-less allocate that overlaps the first attempt waits for
// it and gets the same upload, so one task link never has two.
func TestAllocateUnhashed_OverlappingRetryGetsSameUpload(t *testing.T) {
	p := newPostgresBlob(t)
	pieces := &blockingAllocator{Pieces: pdpfake.NewPieces(), entered: make(chan struct{}), release: make(chan struct{})}
	deps := AllocateDeps{Allocations: p.allocs, Pieces: pieces, Pending: p.allocs, Tx: p.tx}
	req := &AllocateUnhashedRequest{
		Space:      testutil.RandomDID(t),
		DigestCode: multihash.SHA2_256,
		Size:       4,
		Cause:      testutil.RandomCID(t),
		Allocation: testutil.RandomCID(t),
	}

	type result struct {
		resp *AllocateResponse
		err  error
	}
	results := make(chan result, 2)
	allocate := func() {
		resp, err := AllocateUnhashed(t.Context(), deps, req)
		results <- result{resp, err}
	}
	go allocate()
	<-pieces.entered
	go allocate()
	time.Sleep(200 * time.Millisecond)
	require.EqualValues(t, 1, pieces.calls.Load(), "the retry waits for the first attempt")

	close(pieces.release)
	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Equal(t, first.resp.Address.URL, second.resp.Address.URL, "both get the same upload")
	require.EqualValues(t, 1, pieces.calls.Load(), "one upload is made")
}

// An accept that waits for a removal holding the blob's lock finds the bytes
// gone once it commits, and is refused without recording an acceptance.
func TestAccept_AfterRemovalIsRefused(t *testing.T) {
	p := newPostgresBlob(t)
	ctx := t.Context()
	space, digest := testutil.RandomDID(t), testutil.RandomMultihash(t)
	deps := p.acceptDeps()
	pieces := deps.Pieces.(*pdpfake.Pieces)
	pieces.Put(digest, []byte("data"))

	commit := p.rejectFirst(t, func(ctx context.Context) error {
		if err := p.allocs.LockBlob(ctx, digest); err != nil {
			return err
		}
		return pieces.RemovePiece(ctx, digest)
	})
	err := p.acceptAfter(t, deps, &AcceptRequest{
		Space: space,
		Blob:  blob.Blob{Digest: digest, Size: 4},
		Cause: testutil.RandomCID(t),
	}, commit)
	require.ErrorContains(t, err, "piece not found")

	_, err = p.accepts.Get(ctx, digest, space)
	require.ErrorIs(t, err, store.ErrNotFound, "no acceptance is recorded")
}

// An allocate that waits for a removal holding the blob's lock sees the bytes
// gone, and asks for them to be uploaded rather than reporting them held.
func TestAllocate_AfterRemovalAsksForUpload(t *testing.T) {
	p := newPostgresBlob(t)
	ctx := t.Context()
	held := p.allocated(t) // another space's claim, so the bytes are looked for
	pieces := pdpfake.NewPieces()
	pieces.Put(held.Blob.Digest, []byte("data"))
	deps := AllocateDeps{Allocations: p.allocs, Pieces: pieces, Pending: p.allocs, Tx: p.tx}
	space := testutil.RandomDID(t)
	require.NoError(t, p.allocs.Put(ctx, allocation.Allocation{
		Space: space, Blob: held.Blob, Expires: 1, Cause: testutil.RandomCID(t), Allocation: testutil.RandomCID(t),
	}))

	commit := p.rejectFirst(t, func(ctx context.Context) error {
		if err := p.allocs.LockBlob(ctx, held.Blob.Digest); err != nil {
			return err
		}
		return pieces.RemovePiece(ctx, held.Blob.Digest)
	})
	allocated := make(chan *AllocateResponse, 1)
	failed := make(chan error, 1)
	go func() {
		resp, err := Allocate(ctx, deps, &AllocateRequest{
			Space: space, Blob: held.Blob, Cause: testutil.RandomCID(t), Allocation: testutil.RandomCID(t),
		})
		failed <- err
		allocated <- resp
	}()
	select {
	case err := <-failed:
		t.Fatalf("the allocate finished while the removal held the blob: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	commit()
	require.NoError(t, <-failed)
	resp := <-allocated
	require.NotNil(t, resp.Address, "the bytes are gone, so an upload is asked for")
}

// An accept of a digest-less allocation whose allocate task was released and
// allocated again since its digest was resolved is refused: the new
// allocation's upload has not recorded that digest.
func TestAccept_WithoutDigestOfReallocatedTaskIsRefused(t *testing.T) {
	p := newPostgresBlob(t)
	ctx := t.Context()
	resolved := allocation.Pending{
		Allocation: testutil.RandomCID(t),
		Space:      testutil.RandomDID(t),
		Size:       4,
		DigestCode: multihash.SHA2_256,
		Cause:      testutil.RandomCID(t),
		Expires:    1,
		UploadID:   "upload",
		Digest:     testutil.RandomMultihash(t),
	}
	again := resolved
	again.UploadID, again.Digest = "upload-again", nil
	require.NoError(t, p.allocs.PutPending(ctx, again))
	deps := p.acceptDeps()
	deps.Pieces.(*pdpfake.Pieces).Put(resolved.Digest, []byte("data"))

	_, err := Accept(ctx, deps, &AcceptRequest{
		Space:         resolved.Space,
		Blob:          blob.Blob{Digest: resolved.Digest, Size: resolved.Size},
		Cause:         testutil.RandomCID(t),
		Allocation:    &resolved.Allocation,
		WithoutDigest: true,
	})
	require.ErrorContains(t, err, "was released before it was accepted")

	_, err = p.accepts.Get(ctx, resolved.Digest, resolved.Space)
	require.ErrorIs(t, err, store.ErrNotFound, "no acceptance is recorded")
}
