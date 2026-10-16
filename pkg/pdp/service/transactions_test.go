package service

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/fil-forge/libforge/testutil"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	"github.com/fil-forge/piri/pkg/pdp/types"
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
