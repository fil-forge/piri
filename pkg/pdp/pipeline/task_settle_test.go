package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

// stage records blob as staged, as a completed upload without a digest does.
func (p testPipeline) stage(t *testing.T, blob multihash.Multihash) {
	t.Helper()
	_, err := p.db.Exec(t.Context(), `
		INSERT INTO pdp_staged_blobs (digest, upload_id) VALUES ($1, 'upload-1')
	`, []byte(blob))
	require.NoError(t, err)
}

type pipelineRow struct {
	Staged       bool   `db:"staged"`
	SettleTaskID *int64 `db:"settle_task_id"`
	CommPTaskID  *int64 `db:"commp_task_id"`
}

func (p testPipeline) row(t *testing.T, blob multihash.Multihash) pipelineRow {
	t.Helper()
	var rows []pipelineRow
	require.NoError(t, p.db.Select(t.Context(), &rows, `
		SELECT staged, settle_task_id, commp_task_id FROM pdp_blob_pipeline WHERE digest = $1
	`, []byte(blob)))
	require.Len(t, rows, 1)
	return rows[0]
}

// TestEnqueue_StagedBlobStartsAtSettle: a blob still staged when it is
// accepted is settled before commP; any other blob goes straight to commP.
func TestEnqueue_StagedBlobStartsAtSettle(t *testing.T) {
	p := newTestPipeline(t)
	staged, _, _ := testBlobAndPiece(t, "blob-staged")
	p.stage(t, staged)
	require.NoError(t, p.entry.Enqueue(t.Context(), staged))
	row := p.row(t, staged)
	require.True(t, row.Staged)
	require.NotNil(t, row.SettleTaskID, "row claimed by a spawned settle task")
	require.Nil(t, row.CommPTaskID, "commP waits for the settle")

	plain, _, _ := testBlobAndPiece(t, "blob-plain")
	require.NoError(t, p.entry.Enqueue(t.Context(), plain))
	row = p.row(t, plain)
	require.False(t, row.Staged)
	require.Nil(t, row.SettleTaskID)
	require.NotNil(t, row.CommPTaskID)
}

// TestSettleTask_SettlesAndHandsOff: the task settles the blob, records it,
// and hands the row to commP.
func TestSettleTask_SettlesAndHandsOff(t *testing.T) {
	p := newTestPipeline(t)
	blob, _, _ := testBlobAndPiece(t, "blob-settle")
	p.stage(t, blob)
	require.NoError(t, p.entry.Enqueue(t.Context(), blob))

	done, err := p.settle.Do(harmonytask.TaskID(*p.row(t, blob).SettleTaskID), func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, []multihash.Multihash{blob}, p.settler.settled)
	row := p.row(t, blob)
	require.False(t, row.Staged, "the blob is settled")
	require.NotNil(t, row.CommPTaskID, "commP is spawned once the blob is settled")
}

// TestSettleTask_CancelledRowIsNoop: a row the removal sweep cancelled before
// its settle task ran leaves the task nothing to do.
func TestSettleTask_CancelledRowIsNoop(t *testing.T) {
	p := newTestPipeline(t)
	blob, _, _ := testBlobAndPiece(t, "blob-cancelled")
	p.stage(t, blob)
	require.NoError(t, p.entry.Enqueue(t.Context(), blob))
	id := harmonytask.TaskID(*p.row(t, blob).SettleTaskID)
	_, err := p.db.Exec(t.Context(), `DELETE FROM pdp_blob_pipeline WHERE digest = $1`, []byte(blob))
	require.NoError(t, err)

	done, err := p.settle.Do(id, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Empty(t, p.settler.settled)
}

// TestSettleTask_FailedSettleIsRetried: a settle that fails leaves the row
// staged and unclaimed by commP, for the task's retry.
func TestSettleTask_FailedSettleIsRetried(t *testing.T) {
	p := newTestPipeline(t)
	blob, _, _ := testBlobAndPiece(t, "blob-failed")
	p.stage(t, blob)
	require.NoError(t, p.entry.Enqueue(t.Context(), blob))
	p.settler.err = errors.New("object store unreachable")

	done, err := p.settle.Do(harmonytask.TaskID(*p.row(t, blob).SettleTaskID), func() bool { return true })
	require.ErrorIs(t, err, p.settler.err)
	require.False(t, done)
	row := p.row(t, blob)
	require.True(t, row.Staged)
	require.Nil(t, row.CommPTaskID)
}

// TestScavengers_StagedRows: a staged row whose settle spawn was lost is
// picked up by the settle scavenger, and never by the commP scavenger.
func TestScavengers_StagedRows(t *testing.T) {
	p := newTestPipeline(t)
	blob, _, _ := testBlobAndPiece(t, "blob-scavenged")
	_, err := p.db.Exec(context.Background(), `
		INSERT INTO pdp_blob_pipeline (digest, staged) VALUES ($1, true)
	`, []byte(blob))
	require.NoError(t, err)

	require.NoError(t, p.commp.TypeDetails().IAmBored(fakeAdder(t, p.db)))
	require.Nil(t, p.row(t, blob).CommPTaskID, "commP never claims a staged row")

	require.NoError(t, p.settle.TypeDetails().IAmBored(fakeAdder(t, p.db)))
	require.NotNil(t, p.row(t, blob).SettleTaskID, "the settle scavenger claims it")
}

// TestSettleBacklog: the backlog counts staged rows, how long the oldest has
// waited, and those whose settle task is gone from harmony_task.
func TestSettleBacklog(t *testing.T) {
	p := newTestPipeline(t)
	ctx := t.Context()

	b, err := p.settle.backlog(ctx)
	require.NoError(t, err)
	require.Equal(t, settleBacklog{}, b, "an empty pipeline has no backlog")

	insert := func(seed string, staged bool, settleTaskID any, age string) {
		blob, _, _ := testBlobAndPiece(t, seed)
		_, err := p.db.Exec(ctx, `
			INSERT INTO pdp_blob_pipeline (digest, staged, settle_task_id, created_at)
			VALUES ($1, $2, $3, now() - $4::interval)
		`, []byte(blob), staged, settleTaskID, age)
		require.NoError(t, err)
	}
	_, err = p.db.Exec(ctx, `
		INSERT INTO harmony_task (id, posted_time, added_by, name)
		VALUES (7, now(), 1, 'PDPSettle')
	`)
	require.NoError(t, err)
	insert("unclaimed", true, nil, "1 minute")
	insert("running", true, 7, "2 minutes")
	insert("gave-up", true, 8, "1 hour")
	insert("settled", false, 9, "2 hours")

	b, err = p.settle.backlog(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, b.pending, "every staged row counts, settled ones do not")
	require.EqualValues(t, 1, b.abandoned, "only the row whose task is gone has given up")
	require.InDelta(t, time.Hour.Seconds(), b.oldest.Seconds(), 60, "the oldest staged row is an hour old")
}
