package service

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/promise"

	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/pdp/piece"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

// Uploads to an allocation made without a digest are hashed as they are
// received, staged, and moved to the key of the computed digest. These tests
// run against a real harmonydb, since the upload row is the upload's state.

type uploadWorld struct {
	svc *PDPService
	// base is the blobstore under the upload index; bs reads through it, as
	// the node's blobstore does.
	base    blobstore.Blobstore
	bs      *blobstore.StagingStore
	allocs  *allocationstore.Store
	accepts *acceptancestore.Store
}

func setupUploadTest(t *testing.T) *uploadWorld {
	t.Helper()
	db := piritestutil.NewHarmonyDB(t)
	// Upload rows reference the service, as provisioning leaves it.
	_, err := db.Exec(t.Context(), `
		INSERT INTO pdp_services (id, pubkey, service_label)
		VALUES (1, $1, 'storacha') ON CONFLICT DO NOTHING
	`, []byte{1})
	require.NoError(t, err)
	base := blobstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
	w := &uploadWorld{
		base:    base,
		bs:      blobstore.NewStagingStore(base, NewStagingIndex(db)),
		allocs:  allocationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore())),
		accepts: acceptancestore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore())),
	}
	reader, err := piece.NewStoreReader(w.bs)
	require.NoError(t, err)
	w.svc = &PDPService{
		db:              db,
		blobstore:       w.bs,
		allocationStore: w.allocs,
		acceptanceStore: w.accepts,
		pieceReader:     reader,
	}
	return w
}

// allocate makes an allocation without a digest the way the allocate handler
// does: an upload row, then the pending record.
func (w *uploadWorld) allocate(t *testing.T, space did.DID, size int, expires ucan.UnixTimestamp) allocation.Pending {
	t.Helper()
	link := testutil.RandomCID(t)
	alloc, err := w.svc.AllocatePiece(t.Context(), types.PieceAllocation{
		Piece:      types.Piece{Name: "sha2-256", Size: int64(size)},
		Allocation: link,
	})
	require.NoError(t, err)
	require.True(t, alloc.Allocated)
	p := allocation.Pending{
		Allocation: link,
		Space:      space,
		Size:       uint64(size),
		DigestCode: multihash.SHA2_256,
		Cause:      testutil.RandomCID(t),
		Expires:    expires,
		UploadID:   alloc.UploadID.String(),
	}
	require.NoError(t, w.allocs.PutPending(t.Context(), p))
	return p
}

func (w *uploadWorld) upload(t *testing.T, p allocation.Pending, data []byte) error {
	t.Helper()
	return w.svc.UploadPiece(t.Context(), types.PieceUpload{
		ID:   uuid.MustParse(p.UploadID),
		Data: bytes.NewReader(data),
	})
}

// uploadLive reports whether upload id has a row that is not discarded.
func (w *uploadWorld) uploadLive(t *testing.T, id string) bool {
	t.Helper()
	var n int
	require.NoError(t, w.svc.db.QueryRow(t.Context(),
		`SELECT count(*) FROM pdp_piece_uploads WHERE id = $1 AND discarded_at IS NULL`, id).Scan(&n))
	return n > 0
}

// ageDiscards makes every discarded upload old enough for the expiry task to
// reap.
func (w *uploadWorld) ageDiscards(t *testing.T) {
	t.Helper()
	_, err := w.svc.db.Exec(t.Context(),
		`UPDATE pdp_piece_uploads SET discarded_at = $1 WHERE discarded_at IS NOT NULL`,
		time.Now().Add(-2*orphanUploadAge))
	require.NoError(t, err)
}

func (w *uploadWorld) uploadRowExists(t *testing.T, id string) bool {
	t.Helper()
	var n int
	require.NoError(t, w.svc.db.QueryRow(t.Context(),
		`SELECT count(*) FROM pdp_piece_uploads WHERE id = $1`, id).Scan(&n))
	return n > 0
}

func (w *uploadWorld) stored(t *testing.T, digest multihash.Multihash) []byte {
	t.Helper()
	obj, err := w.bs.Get(t.Context(), digest)
	require.NoError(t, err)
	b, err := io.ReadAll(obj.Body())
	require.NoError(t, err)
	return b
}

// uploadData reports whether upload id's data is stored.
func (w *uploadWorld) uploadData(t *testing.T, id string) bool {
	t.Helper()
	obj, err := w.bs.GetStaged(t.Context(), id)
	if err == nil {
		_ = obj.Body().Close()
		return true
	}
	require.ErrorIs(t, err, store.ErrNotFound)
	return false
}

// atDigestKey reports whether the blob is stored under its digest.
func (w *uploadWorld) atDigestKey(t *testing.T, digest multihash.Multihash) bool {
	t.Helper()
	obj, err := w.base.Get(t.Context(), digest)
	if err == nil {
		_ = obj.Body().Close()
		return true
	}
	require.ErrorIs(t, err, store.ErrNotFound)
	return false
}

// holder returns the upload the index records as holding the blob.
func (w *uploadWorld) holder(t *testing.T, digest multihash.Multihash) (string, bool) {
	t.Helper()
	id, ok, err := NewStagingIndex(w.svc.db).GetID(t.Context(), digest)
	require.NoError(t, err)
	return id, ok
}

func day() ucan.UnixTimestamp { return ucan.Now() + 60*60*24 }

func TestUploadUnhashed(t *testing.T) {
	w := setupUploadTest(t)
	space := testutil.RandomDID(t)
	data := testutil.RandomBytes(t, 256)
	digest := mustMultihash(t, string(data))

	p := w.allocate(t, space, len(data), day())
	require.NoError(t, w.upload(t, p, data))

	got, err := w.allocs.GetPending(t.Context(), p.Allocation)
	require.NoError(t, err)
	require.Equal(t, digest, got.Digest, "the computed digest is recorded")

	alloc, err := w.allocs.Get(t.Context(), digest, space)
	require.NoError(t, err, "the upload claims (digest, space)")
	require.Equal(t, p.Cause, alloc.Cause)
	require.Equal(t, uint64(len(data)), alloc.Blob.Size)

	holder, ok := w.holder(t, digest)
	require.True(t, ok, "the upload is recorded as holding the blob")
	require.Equal(t, p.UploadID, holder)
	require.True(t, w.uploadData(t, p.UploadID), "the data stays under the upload's key")
	require.False(t, w.atDigestKey(t, digest), "nothing is copied to the digest key")
	require.Equal(t, data, w.stored(t, digest), "the blob reads by its digest")
	require.False(t, w.uploadRowExists(t, p.UploadID), "the upload is complete")

	has, err := w.svc.Has(t.Context(), digest)
	require.NoError(t, err)
	require.True(t, has)
}

// TestUploadUnhashed_SettleThenCommP: a staged blob reads through the staging
// store, commP included, until it is settled at its digest; settling it
// changes nothing a reader sees.
func TestUploadUnhashed_SettleThenCommP(t *testing.T) {
	w := setupUploadTest(t)
	data := testutil.RandomBytes(t, 4096)
	digest := mustMultihash(t, string(data))
	p := w.allocate(t, testutil.RandomDID(t), len(data), day())
	require.NoError(t, w.upload(t, p, data))

	staged, err := w.svc.CalculateCommP(t.Context(), digest)
	require.NoError(t, err)
	require.Equal(t, int64(len(data)), staged.RawSize)
	require.False(t, w.atDigestKey(t, digest), "commP reads a staged blob without settling it")

	require.NoError(t, w.bs.Settle(t.Context(), digest))
	require.True(t, w.atDigestKey(t, digest))
	require.False(t, w.uploadData(t, p.UploadID), "the blob is unstaged")
	_, ok := w.holder(t, digest)
	require.False(t, ok, "the index no longer names the upload")
	require.Equal(t, data, w.stored(t, digest))

	_, err = w.svc.db.Exec(t.Context(), `DELETE FROM pdp_piece_mh_to_commp WHERE mhash = $1`, []byte(digest))
	require.NoError(t, err)
	settled, err := w.svc.CalculateCommP(t.Context(), digest)
	require.NoError(t, err)
	require.Equal(t, staged.PieceCID, settled.PieceCID, "commP is the same read from the digest key")
}

func TestUploadUnhashed_DiscardKeepsHeldBlob(t *testing.T) {
	w := setupUploadTest(t)
	data := testutil.RandomBytes(t, 256)
	digest := mustMultihash(t, string(data))
	p := w.allocate(t, testutil.RandomDID(t), len(data), day())
	require.NoError(t, w.upload(t, p, data))

	require.NoError(t, w.svc.DiscardUpload(t.Context(), p.UploadID))
	require.True(t, w.uploadData(t, p.UploadID), "a completed upload's data is the blob's")
	require.Equal(t, data, w.stored(t, digest))

	require.NoError(t, w.svc.blobstore.Delete(t.Context(), digest))
	require.False(t, w.uploadData(t, p.UploadID), "removing the blob removes the upload's data")
	_, ok := w.holder(t, digest)
	require.False(t, ok)
}

// discardingAllocations runs discard once, just before the upload claims
// (digest, space): after its data is staged and before it is recorded as the
// staged copy of its blob.
type discardingAllocations struct {
	allocationstore.AllocationStore
	discard func()
}

func (d *discardingAllocations) Claim(ctx context.Context, alloc allocation.Allocation) (bool, error) {
	if f := d.discard; f != nil {
		d.discard = nil
		f()
	}
	return d.AllocationStore.Claim(ctx, alloc)
}

// TestUploadUnhashed_DiscardedWhileCompleting: the allocation is rejected
// while its upload completes. The reject discards the upload and releases the
// pending record it read before the digest was recorded. Neither side may
// leave an entry for staged bytes that are gone, or a claim nothing releases.
func TestUploadUnhashed_DiscardedWhileCompleting(t *testing.T) {
	w := setupUploadTest(t)
	ctx := t.Context()
	space := testutil.RandomDID(t)
	data := testutil.RandomBytes(t, 256)
	digest := mustMultihash(t, string(data))
	p := w.allocate(t, space, len(data), day())
	w.svc.allocationStore = &discardingAllocations{AllocationStore: w.allocs, discard: func() {
		require.NoError(t, w.svc.DiscardUpload(ctx, p.UploadID))
		_, err := w.allocs.ReleasePending(ctx, p)
		require.NoError(t, err)
	}}

	require.Error(t, w.upload(t, p, data), "the upload's allocation was released")

	_, ok := w.holder(t, digest)
	require.False(t, ok, "no entry for a discarded upload")
	require.False(t, w.uploadData(t, p.UploadID), "the discarded upload's data is gone")
	require.False(t, w.uploadRowExists(t, p.UploadID))
	_, err := w.allocs.Get(ctx, digest, space)
	require.ErrorIs(t, err, store.ErrNotFound, "no claim is left on the digest")
	_, err = w.allocs.GetPending(ctx, p.Allocation)
	require.ErrorIs(t, err, store.ErrNotFound, "no pending record is left")
}

// TestUploadUnhashed_DuplicateDiscardedWhileCompleting: the same, for an
// upload of content another upload already staged. The discard releases only
// what the duplicate recorded; the staged copy and its entry stay.
func TestUploadUnhashed_DuplicateDiscardedWhileCompleting(t *testing.T) {
	w := setupUploadTest(t)
	ctx := t.Context()
	data := testutil.RandomBytes(t, 256)
	digest := mustMultihash(t, string(data))
	first := w.allocate(t, testutil.RandomDID(t), len(data), day())
	require.NoError(t, w.upload(t, first, data))

	space := testutil.RandomDID(t)
	dup := w.allocate(t, space, len(data), day())
	w.svc.allocationStore = &discardingAllocations{AllocationStore: w.allocs, discard: func() {
		require.NoError(t, w.svc.DiscardUpload(ctx, dup.UploadID))
		_, err := w.allocs.ReleasePending(ctx, dup)
		require.NoError(t, err)
	}}

	require.Error(t, w.upload(t, dup, data), "the duplicate's allocation was released")

	holder, ok := w.holder(t, digest)
	require.True(t, ok)
	require.Equal(t, first.UploadID, holder, "the first upload still holds the blob")
	require.Equal(t, data, w.stored(t, digest))
	require.False(t, w.uploadData(t, dup.UploadID))
	require.False(t, w.uploadRowExists(t, dup.UploadID))
	_, err := w.allocs.Get(ctx, digest, space)
	require.ErrorIs(t, err, store.ErrNotFound, "the duplicate's claim is released")
}

// pausingReader hands out its data, pausing at its first read until release
// is closed.
type pausingReader struct {
	r       io.Reader
	reading chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *pausingReader) Read(b []byte) (int, error) {
	p.once.Do(func() {
		close(p.reading)
		<-p.release
	})
	return p.r.Read(b)
}

// TestUploadUnhashed_RetriedPutWhileReceiving: a client retries its PUT while
// the first is still being received. The retry waits for the first, which
// completes the upload; the retry is then refused, and undoes nothing.
func TestUploadUnhashed_RetriedPutWhileReceiving(t *testing.T) {
	w := setupUploadTest(t)
	ctx := t.Context()
	space := testutil.RandomDID(t)
	data := testutil.RandomBytes(t, 256)
	digest := mustMultihash(t, string(data))
	p := w.allocate(t, space, len(data), day())
	id := uuid.MustParse(p.UploadID)

	first := &pausingReader{r: bytes.NewReader(data), reading: make(chan struct{}), release: make(chan struct{})}
	firstDone := make(chan error, 1)
	go func() { firstDone <- w.svc.UploadPiece(ctx, types.PieceUpload{ID: id, Data: first}) }()
	<-first.reading

	retryDone := make(chan error, 1)
	go func() { retryDone <- w.svc.UploadPiece(ctx, types.PieceUpload{ID: id, Data: bytes.NewReader(data)}) }()
	select {
	case err := <-retryDone:
		retryDone <- err
	case <-time.After(100 * time.Millisecond):
	}
	close(first.release)

	errs := []error{<-firstDone, <-retryDone}
	require.NoError(t, errs[0], "the first PUT completes the upload")
	require.Error(t, errs[1], "the retry finds the upload complete")

	holder, ok := w.holder(t, digest)
	require.True(t, ok)
	require.Equal(t, p.UploadID, holder)
	require.Equal(t, data, w.stored(t, digest), "the staged data is intact")
	got, err := w.allocs.GetPending(ctx, p.Allocation)
	require.NoError(t, err, "the pending allocation is kept")
	require.Equal(t, digest, got.Digest)
	_, err = w.allocs.Get(ctx, digest, space)
	require.NoError(t, err, "the claim is kept")
}

func TestUploadUnhashed_ContentAlreadyHeld(t *testing.T) {
	w := setupUploadTest(t)
	space := testutil.RandomDID(t)
	data := testutil.RandomBytes(t, 256)
	digest := mustMultihash(t, string(data))

	first := w.allocate(t, space, len(data), day())
	require.NoError(t, w.upload(t, first, data))
	second := w.allocate(t, space, len(data), day())
	require.NoError(t, w.upload(t, second, data))

	alloc, err := w.allocs.Get(t.Context(), digest, space)
	require.NoError(t, err)
	require.Equal(t, first.Cause, alloc.Cause, "the first upload keeps the claim")

	got, err := w.allocs.GetPending(t.Context(), second.Allocation)
	require.NoError(t, err)
	require.Equal(t, digest, got.Digest)

	holder, ok := w.holder(t, digest)
	require.True(t, ok)
	require.Equal(t, first.UploadID, holder, "the first upload holds the blob")
	require.Equal(t, data, w.stored(t, digest))
	require.False(t, w.uploadData(t, second.UploadID), "the duplicate is dropped")
	require.False(t, w.uploadRowExists(t, second.UploadID))
}

func TestUploadUnhashed_WrongSize(t *testing.T) {
	w := setupUploadTest(t)
	data := testutil.RandomBytes(t, 256)

	for name, body := range map[string][]byte{
		"short": data[:255],
		"long":  append(append([]byte(nil), data...), 0),
	} {
		t.Run(name, func(t *testing.T) {
			p := w.allocate(t, testutil.RandomDID(t), len(data), day())
			err := w.upload(t, p, body)
			var perr *types.Error
			require.ErrorAs(t, err, &perr)
			require.Equal(t, types.KindPayloadTooLarge, perr.Kind())

			got, err := w.allocs.GetPending(t.Context(), p.Allocation)
			require.NoError(t, err)
			require.Empty(t, got.Digest, "no digest is recorded")
			require.False(t, w.uploadData(t, p.UploadID), "the partial upload is dropped")
			require.True(t, w.uploadRowExists(t, p.UploadID), "the upload can be retried")

			require.NoError(t, w.upload(t, p, data), "the retry succeeds")
		})
	}
}

func TestProcessExpiredAllocations(t *testing.T) {
	w := setupUploadTest(t)
	ctx := t.Context()
	past := ucan.Now() - 1
	data := testutil.RandomBytes(t, 256)
	digest := mustMultihash(t, string(data))

	// Expired before any data arrived.
	idle := w.allocate(t, testutil.RandomDID(t), 64, past)

	// Expired after the data arrived, never accepted: parked, like an
	// allocation made with a digest.
	space := testutil.RandomDID(t)
	received := w.allocate(t, space, len(data), past)
	require.NoError(t, w.upload(t, received, data))

	// Expired and accepted: the acceptance holds the claim now.
	acceptedData := testutil.RandomBytes(t, 256)
	acceptedDigest := mustMultihash(t, string(acceptedData))
	acceptedSpace := testutil.RandomDID(t)
	accepted := w.allocate(t, acceptedSpace, len(acceptedData), past)
	require.NoError(t, w.upload(t, accepted, acceptedData))
	require.NoError(t, w.accepts.Put(ctx, acceptance.Acceptance{
		Space:      acceptedSpace,
		Blob:       acceptance.Blob{Digest: acceptedDigest, Size: uint64(len(acceptedData))},
		PDPAccept:  promise.AwaitOK{Task: testutil.RandomCID(t)},
		Cause:      testutil.RandomCID(t),
		Site:       testutil.RandomCID(t),
		Allocation: &accepted.Allocation,
	}))

	// Not yet expired.
	live := w.allocate(t, testutil.RandomDID(t), 64, day())

	// An upload row whose pending record was never written.
	orphan, err := w.svc.AllocatePiece(ctx, types.PieceAllocation{
		Piece:      types.Piece{Name: "sha2-256", Size: 64},
		Allocation: testutil.RandomCID(t),
	})
	require.NoError(t, err)
	_, err = w.svc.db.Exec(ctx, `UPDATE pdp_piece_uploads SET created_at = $1 WHERE id = $2`,
		time.Now().Add(-2*orphanUploadAge), orphan.UploadID.String())
	require.NoError(t, err)

	require.NoError(t, w.svc.ProcessExpiredAllocations(ctx))

	for _, p := range []allocation.Pending{idle, accepted} {
		_, err := w.allocs.GetPending(ctx, p.Allocation)
		require.ErrorIs(t, err, store.ErrNotFound, "an expired allocation that was idle or accepted loses its pending record")
	}
	require.False(t, w.uploadLive(t, idle.UploadID), "the idle upload is discarded")

	// The received upload stays parked: its pending record, its claim and its
	// data are all kept, however long it has been waiting for an accept.
	kept, err := w.allocs.GetPending(ctx, received.Allocation)
	require.NoError(t, err, "a received allocation keeps its pending record past its expiry")
	require.Equal(t, digest, kept.Digest)
	_, err = w.allocs.Get(ctx, digest, space)
	require.NoError(t, err, "the received upload keeps its claim")
	require.True(t, w.uploadData(t, received.UploadID), "the received upload keeps its data")
	var queued int
	require.NoError(t, w.svc.db.QueryRow(ctx,
		`SELECT count(*) FROM pdp_pending_piece_removals WHERE digest = $1`, []byte(digest)).Scan(&queued))
	require.Zero(t, queued, "nothing is queued for removal")

	_, err = w.allocs.Get(ctx, acceptedDigest, acceptedSpace)
	require.NoError(t, err, "an accepted upload keeps its claim")

	_, err = w.allocs.GetPending(ctx, live.Allocation)
	require.NoError(t, err, "a live allocation is kept")
	require.True(t, w.uploadRowExists(t, live.UploadID))

	require.False(t, w.uploadLive(t, orphan.UploadID.String()), "the orphaned upload is discarded")

	w.ageDiscards(t)
	require.NoError(t, w.svc.ProcessExpiredAllocations(ctx))
	require.False(t, w.uploadRowExists(t, idle.UploadID), "a discarded upload's row is reaped")
	require.False(t, w.uploadRowExists(t, orphan.UploadID.String()))
	require.True(t, w.uploadRowExists(t, live.UploadID))

	// An explicit release still undoes the parked allocation.
	require.NoError(t, w.svc.DiscardUpload(ctx, received.UploadID))
	released, err := w.allocs.ReleasePending(ctx, received)
	require.NoError(t, err)
	require.Equal(t, digest, released.Digest)
	_, err = w.allocs.Get(ctx, digest, space)
	require.ErrorIs(t, err, store.ErrNotFound, "releasing the parked allocation releases its claim")
}

// TestUploadUnhashed_DiscardedWhileWriting: the allocation is rejected while
// its upload is still being written, and the upload stops before it completes.
// The data it wrote after the discard is found and dropped by the expiry task;
// an upload that arrives after the discard is refused.
func TestUploadUnhashed_DiscardedWhileWriting(t *testing.T) {
	w := setupUploadTest(t)
	ctx := t.Context()
	data := testutil.RandomBytes(t, 256)
	p := w.allocate(t, testutil.RandomDID(t), len(data), day())

	require.NoError(t, w.svc.DiscardUpload(ctx, p.UploadID))
	// The upload's write lands after the discard, and the upload stops there.
	require.NoError(t, w.bs.Stage(ctx, p.UploadID, uint64(len(data)), bytes.NewReader(data)))
	require.True(t, w.uploadRowExists(t, p.UploadID), "the discarded row is kept while its upload may still write")

	require.Error(t, w.upload(t, p, data), "an upload to a discarded row is refused")

	w.ageDiscards(t)
	require.NoError(t, w.svc.ProcessExpiredAllocations(ctx))
	require.False(t, w.uploadData(t, p.UploadID), "the data written after the discard is dropped")
	require.False(t, w.uploadRowExists(t, p.UploadID))
}
