package service

import (
	"bytes"
	"io"
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

	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/pdp/piece"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store"
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
	base   blobstore.Blobstore
	bs     blobstore.Blobstore
	allocs *allocationstore.Store
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
		base:   base,
		bs:     blobstore.WithUploads(base, NewUploadIndex(db)),
		allocs: allocationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore())),
	}
	reader, err := piece.NewStoreReader(w.bs)
	require.NoError(t, err)
	w.svc = &PDPService{
		db:              db,
		blobstore:       w.bs,
		allocationStore: w.allocs,
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
	obj, err := w.base.GetUpload(t.Context(), id)
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
	id, ok, err := NewUploadIndex(w.svc.db).Upload(t.Context(), digest)
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

func TestUploadUnhashed_CommPSettles(t *testing.T) {
	w := setupUploadTest(t)
	data := testutil.RandomBytes(t, 4096)
	digest := mustMultihash(t, string(data))
	p := w.allocate(t, testutil.RandomDID(t), len(data), day())
	require.NoError(t, w.upload(t, p, data))

	res, err := w.svc.CalculateCommP(t.Context(), digest)
	require.NoError(t, err)
	require.Equal(t, int64(len(data)), res.RawSize)

	require.True(t, w.atDigestKey(t, digest), "the commP read settles the blob at its digest")
	require.False(t, w.uploadData(t, p.UploadID), "the upload's data is gone")
	_, ok := w.holder(t, digest)
	require.False(t, ok, "the index no longer names the upload")
	require.Equal(t, data, w.stored(t, digest))

	again, err := w.svc.CalculateCommP(t.Context(), digest)
	require.NoError(t, err)
	require.Equal(t, res.PieceCID, again.PieceCID, "commP is the same read from the digest key")
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

	// Expired after the data arrived, never accepted.
	space := testutil.RandomDID(t)
	received := w.allocate(t, space, len(data), past)
	require.NoError(t, w.upload(t, received, data))

	// Expired and accepted: the acceptance holds the claim now.
	acceptedData := testutil.RandomBytes(t, 256)
	acceptedDigest := mustMultihash(t, string(acceptedData))
	acceptedSpace := testutil.RandomDID(t)
	accepted := w.allocate(t, acceptedSpace, len(acceptedData), past)
	require.NoError(t, w.upload(t, accepted, acceptedData))
	accepted, err := w.allocs.GetPending(ctx, accepted.Allocation)
	require.NoError(t, err)
	accepted.Accepted = true
	require.NoError(t, w.allocs.PutPending(ctx, accepted))

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

	for _, p := range []allocation.Pending{idle, received, accepted} {
		_, err := w.allocs.GetPending(ctx, p.Allocation)
		require.ErrorIs(t, err, store.ErrNotFound, "expired pending allocations are deleted")
	}
	require.False(t, w.uploadRowExists(t, idle.UploadID), "the idle upload is discarded")

	_, err = w.allocs.Get(ctx, digest, space)
	require.ErrorIs(t, err, store.ErrNotFound, "the received upload's claim is released")
	var queued int
	require.NoError(t, w.svc.db.QueryRow(ctx,
		`SELECT count(*) FROM pdp_pending_piece_removals WHERE digest = $1`, []byte(digest)).Scan(&queued))
	require.Equal(t, 1, queued, "the released bytes are queued for removal")

	_, err = w.allocs.Get(ctx, acceptedDigest, acceptedSpace)
	require.NoError(t, err, "an accepted upload keeps its claim")

	_, err = w.allocs.GetPending(ctx, live.Allocation)
	require.NoError(t, err, "a live allocation is kept")
	require.True(t, w.uploadRowExists(t, live.UploadID))

	require.False(t, w.uploadRowExists(t, orphan.UploadID.String()), "the orphaned upload is discarded")
}
