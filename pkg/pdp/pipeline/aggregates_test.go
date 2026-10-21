package pipeline

import (
	"context"
	"testing"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	libpiece "github.com/fil-forge/libforge/piece"

	"github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/aggregator"
	aggtypes "github.com/fil-forge/piri/pkg/pdp/aggregation/types"
)

// recordingSubmitter records the roots the aggregation task submits.
type recordingSubmitter struct{ roots []cid.Cid }

func (s *recordingSubmitter) Submit(_ context.Context, roots ...cid.Cid) error {
	s.roots = append(s.roots, roots...)
	return nil
}

// pieceRow is a pipeline row whose commP is done: the blob of seed with the
// piece of pieceSeed.
type pieceRow struct{ seed, pieceSeed string }

// insertPieceRows inserts rows in order of creation and returns their pieces
// in the same order.
func insertPieceRows(t *testing.T, db *harmonydb.DB, rows []pieceRow) []libpiece.Piece {
	t.Helper()
	pieces := make([]libpiece.Piece, len(rows))
	for i, r := range rows {
		blob, _, _ := testBlobAndPiece(t, r.seed)
		_, commp, _ := testBlobAndPiece(t, r.pieceSeed)
		_, err := db.Exec(t.Context(), `
			INSERT INTO pdp_blob_pipeline (digest, commp, created_at)
			VALUES ($1, $2, now() - make_interval(secs => $3::double precision))
		`, []byte(blob), commp, len(rows)-i)
		require.NoError(t, err)
		c, err := cid.Parse(commp)
		require.NoError(t, err)
		pieces[i], err = libpiece.FromCID(c)
		require.NoError(t, err)
	}
	return pieces
}

// foldAll runs the aggregation task with a threshold that folds every given
// piece into one aggregate, and returns the aggregate the fold computed.
func foldAll(t *testing.T, db *harmonydb.DB, pieces []libpiece.Piece) aggtypes.Aggregate {
	t.Helper()
	var size uint64
	for _, p := range pieces {
		size += p.PaddedSize()
	}
	policy := aggregator.NewPolicy(func() uint64 { return size })
	want, err := aggregator.Append(pieces, policy)
	require.NoError(t, err)
	require.Len(t, want, 1)

	submitter := &recordingSubmitter{}
	done, err := NewAggregateTask(db, submitter, policy).Do(1, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, []cid.Cid{want[0].Root}, submitter.roots)
	return want[0]
}

// TestAggregate_RoundTrip: the aggregate rebuilt from the pipeline rows is
// the one the fold computed, with piece order and inclusion proofs intact,
// including when two blobs share a piece.
func TestAggregate_RoundTrip(t *testing.T) {
	db := testutil.NewHarmonyDB(t)
	pieces := insertPieceRows(t, db, []pieceRow{
		{seed: "blob-a", pieceSeed: "piece-1"},
		{seed: "blob-b", pieceSeed: "piece-2"},
		{seed: "blob-c", pieceSeed: "piece-1"},
	})
	want := foldAll(t, db, pieces)

	got, err := loadAggregate(t.Context(), db, want.Root)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// TestAddRoots_RetireRemovesAggregate: retiring a submitted batch deletes
// the aggregates stored on its pipeline rows.
func TestAddRoots_RetireRemovesAggregate(t *testing.T) {
	db := testutil.NewHarmonyDB(t)
	pieces := insertPieceRows(t, db, []pieceRow{
		{seed: "blob-a", pieceSeed: "piece-1"},
		{seed: "blob-b", pieceSeed: "piece-2"},
	})
	a := foldAll(t, db, pieces)

	task := &AddRootsTask{db: db}
	require.NoError(t, task.retire(t.Context(), 7, []string{a.Root.String()}))

	_, err := loadAggregate(t.Context(), db, a.Root)
	require.ErrorContains(t, err, "has no pieces")
}

// TestAddRoots_NoSubmissionRows: a task whose submission rows are gone, as
// after the 00000008 migration, completes without doing anything.
func TestAddRoots_NoSubmissionRows(t *testing.T) {
	db := testutil.NewHarmonyDB(t)
	done, err := (&AddRootsTask{db: db}).Do(7, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
}

// The 00000008 migration unfolds aggregated rows and clears their
// agg_task_id, so the aggregation task's IAmBored claims them again. A row
// that kept a stale agg_task_id would wait for a fold spawned by another
// piece.
func TestAggregateBored_ClaimsUnfoldedRows(t *testing.T) {
	cases := map[string]struct {
		aggTaskID any
		want      *int64
	}{
		"agg_task_id cleared":      {aggTaskID: nil, want: ptr(int64(1))},
		"agg_task_id left at 1234": {aggTaskID: 1234, want: ptr(int64(1234))},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db := testutil.NewHarmonyDB(t)
			blob, commp, _ := testBlobAndPiece(t, "blob-unfolded")
			_, err := db.Exec(t.Context(), `
				INSERT INTO pdp_blob_pipeline (digest, commp, agg_task_id) VALUES ($1, $2, $3)
			`, []byte(blob), commp, tc.aggTaskID)
			require.NoError(t, err)

			task := NewAggregateTask(db, nil, aggregator.Policy{})
			require.NoError(t, task.TypeDetails().IAmBored(fakeAdder(t, db)))

			var aggTaskID *int64
			require.NoError(t, db.QueryRow(t.Context(), `
				SELECT agg_task_id FROM pdp_blob_pipeline WHERE digest = $1
			`, []byte(blob)).Scan(&aggTaskID))
			require.Equal(t, tc.want, aggTaskID)
		})
	}
}

func ptr[T any](v T) *T { return &v }
