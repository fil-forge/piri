package service

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

var errRollback = errors.New("roll back")

// An upload record written in a transaction that rolls back is gone with it,
// so an allocate that fails after AllocatePiece leaves no upload behind.
func TestAllocatePiece_RollsBackWithTransaction(t *testing.T) {
	w := setupUploadTest(t)
	ctx := t.Context()
	link := testutil.RandomCID(t)

	err := harmonytx.Run(ctx, w.svc.db, func(ctx context.Context, _ *harmonydb.Tx) error {
		_, err := w.svc.AllocatePiece(ctx, types.PieceAllocation{
			Piece:      types.Piece{Name: "sha2-256", Size: 64},
			Allocation: link,
		})
		require.NoError(t, err)
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)

	var n int
	require.NoError(t, w.svc.db.QueryRow(ctx,
		`SELECT count(*) FROM pdp_piece_uploads WHERE allocation = $1`, link.String()).Scan(&n))
	require.Zero(t, n)
}

// A discard in a transaction marks the upload and drops its data only once
// the transaction commits: one that rolls back leaves both as they were.
func TestDiscardUpload_DropsDataAfterCommit(t *testing.T) {
	w := setupUploadTest(t)
	ctx := t.Context()
	p := w.allocate(t, testutil.RandomDID(t), 4, day())
	require.NoError(t, w.bs.Stage(ctx, p.UploadID, 4, bytes.NewReader([]byte("data"))))

	err := harmonytx.Run(ctx, w.svc.db, func(ctx context.Context, _ *harmonydb.Tx) error {
		require.NoError(t, w.svc.DiscardUpload(ctx, p.UploadID))
		require.True(t, w.uploadData(t, p.UploadID), "the data stays until the commit")
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
	require.True(t, w.uploadLive(t, p.UploadID), "the mark is rolled back")
	require.True(t, w.uploadData(t, p.UploadID), "the data is kept")

	require.NoError(t, harmonytx.Run(ctx, w.svc.db, func(ctx context.Context, _ *harmonydb.Tx) error {
		return w.svc.DiscardUpload(ctx, p.UploadID)
	}))
	require.False(t, w.uploadLive(t, p.UploadID))
	require.False(t, w.uploadData(t, p.UploadID), "the data goes once the discard commits")
}

// An allocation made again under the same allocate task after the expiry
// task listed it has a new expiry, and is not expired with the listed one.
func TestExpireAllocation_SkipsAllocationMadeAgain(t *testing.T) {
	w := setupUploadTest(t)
	ctx := t.Context()
	listed := w.allocate(t, testutil.RandomDID(t), 64, ucan.Now()-1)

	again := listed
	again.Expires = day()
	require.NoError(t, w.allocs.DeletePending(ctx, listed.Allocation))
	require.NoError(t, w.allocs.PutPending(ctx, again))

	require.NoError(t, w.svc.expireAllocation(ctx, listed))

	got, err := w.allocs.GetPending(ctx, listed.Allocation)
	require.NoError(t, err, "the allocation made again is kept")
	require.Equal(t, again.Expires, got.Expires)
	require.True(t, w.uploadLive(t, listed.UploadID), "its upload is not discarded")
}

// A blob whose removal is queued and whose accept holds the blob's lock with
// its acceptance not yet committed: finalization waits for the accept, then
// sees the acceptance, cancels the removal and keeps the bytes.
func TestFinalizeRemoval_WaitsForAccept(t *testing.T) {
	db := piritestutil.NewHarmonyDB(t)
	ctx := t.Context()
	bs := blobstore.NewStagingStore(blobstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore())), NewStagingIndex(db))
	accepts := acceptancestore.NewPostgresStore(db)
	allocs := allocationstore.NewPostgresStore(db)
	svc := &PDPService{db: db, blobstore: bs, acceptanceStore: accepts, allocationStore: allocs}

	data := []byte("revived")
	digest := mustMultihash(t, string(data))
	require.NoError(t, bs.Put(ctx, digest, uint64(len(data)), bytes.NewReader(data)))
	require.NoError(t, svc.RemovePiece(ctx, digest))

	locked, finish := make(chan struct{}), make(chan struct{})
	accepted := make(chan error, 1)
	go func() {
		accepted <- harmonytx.Run(ctx, db, func(ctx context.Context, _ *harmonydb.Tx) error {
			if err := allocs.LockBlob(ctx, digest); err != nil {
				return err
			}
			if err := accepts.Put(ctx, acceptance.Acceptance{
				Space:     testutil.RandomDID(t),
				Blob:      acceptance.Blob{Digest: digest, Size: uint64(len(data))},
				PDPAccept: promise.AwaitOK{Task: testutil.RandomCID(t)},
				Cause:     testutil.RandomCID(t),
				Site:      testutil.RandomCID(t),
			}); err != nil {
				return err
			}
			close(locked)
			<-finish
			return nil
		})
	}()
	<-locked

	finalized := make(chan error, 1)
	go func() { finalized <- svc.finalizeRemoval(ctx, digest, "") }()
	// Without the blob's lock, finalization finishes well within this, and
	// deletes the bytes the accept is claiming.
	select {
	case err := <-finalized:
		t.Fatalf("finalization finished while an accept held the blob: %v", err)
	case <-time.After(time.Second):
	}

	close(finish)
	require.NoError(t, <-accepted)
	require.NoError(t, <-finalized)
	obj, err := bs.Get(ctx, digest)
	require.NoError(t, err, "the accepted bytes are kept")
	require.NoError(t, obj.Body().Close())
	var queued int
	require.NoError(t, db.QueryRow(ctx,
		`SELECT count(*) FROM pdp_pending_piece_removals WHERE digest = $1`, []byte(digest)).Scan(&queued))
	require.Zero(t, queued, "the removal is cancelled")
}
