package pipeline

import (
	"testing"
	"time"

	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

// These tests pin the commP delay: an accepted blob's pipeline row is claimed
// by a commP task only once the delay has passed since its creation, by the
// task's IAmBored rather than a spawn at acceptance, and the delay in force
// at claim time is what counts.

func (p testPipeline) bored(t *testing.T) {
	t.Helper()
	require.NoError(t, p.commp.TypeDetails().IAmBored(fakeAdder(t, p.db)))
}

// TestEnqueue_HeldUntilDue: with a delay, Enqueue inserts the row unclaimed,
// IAmBored leaves it alone until the delay has passed, then claims it.
func TestEnqueue_HeldUntilDue(t *testing.T) {
	p := newTestPipeline(t)
	*p.delay = time.Hour
	blob, _, _ := testBlobAndPiece(t, "blob-held")

	require.NoError(t, p.entry.Enqueue(t.Context(), blob))
	require.Nil(t, p.row(t, blob).CommPTaskID, "no commP task is spawned at acceptance")

	p.bored(t)
	require.Nil(t, p.row(t, blob).CommPTaskID, "a row inside the delay is not claimed")

	p.backdate(t, blob, 2*time.Hour)
	p.bored(t)
	require.NotNil(t, p.row(t, blob).CommPTaskID, "a row past the delay is claimed")
}

// TestSpawn_LeavesHeldRow: a direct spawn (the settle task's hand-off) claims
// nothing while the row is inside the delay, and claims it once it is due.
func TestSpawn_LeavesHeldRow(t *testing.T) {
	p := newTestPipeline(t)
	*p.delay = time.Hour
	blob, _, _ := testBlobAndPiece(t, "blob-spawn-held")
	require.NoError(t, p.entry.Enqueue(t.Context(), blob))

	p.commp.spawn(t.Context(), blob)
	require.Nil(t, p.row(t, blob).CommPTaskID)

	p.backdate(t, blob, 2*time.Hour)
	p.commp.spawn(t.Context(), blob)
	require.NotNil(t, p.row(t, blob).CommPTaskID)
}

// TestDelayChange_AppliesToWaitingRows: the delay is read at claim time, so
// dropping it to zero releases every waiting row on the next IAmBored.
func TestDelayChange_AppliesToWaitingRows(t *testing.T) {
	p := newTestPipeline(t)
	*p.delay = time.Hour
	blob, _, _ := testBlobAndPiece(t, "blob-retuned")
	require.NoError(t, p.entry.Enqueue(t.Context(), blob))
	p.bored(t)
	require.Nil(t, p.row(t, blob).CommPTaskID)

	*p.delay = 0
	p.bored(t)
	require.NotNil(t, p.row(t, blob).CommPTaskID, "a zero delay makes the row due at once")
}

// TestBored_ClaimsEveryDueRow: one IAmBored call claims every due row, not
// one, and leaves the rows still inside the delay.
func TestBored_ClaimsEveryDueRow(t *testing.T) {
	p := newTestPipeline(t)
	*p.delay = time.Hour
	var due []multihash.Multihash
	for _, seed := range []string{"due-1", "due-2", "due-3"} {
		blob, _, _ := testBlobAndPiece(t, seed)
		require.NoError(t, p.entry.Enqueue(t.Context(), blob))
		p.backdate(t, blob, 90*time.Minute)
		due = append(due, blob)
	}
	held, _, _ := testBlobAndPiece(t, "held")
	require.NoError(t, p.entry.Enqueue(t.Context(), held))

	p.bored(t)

	for _, blob := range due {
		require.NotNil(t, p.row(t, blob).CommPTaskID, "every due row is claimed in one pass")
	}
	require.Nil(t, p.row(t, held).CommPTaskID)
	_, claimed := pipelineRows(t, p.db)
	require.Equal(t, 3, claimed)
}

// TestCommPBacklog: the backlog splits unclaimed, settled rows into held and
// due against the current delay, and measures how long the oldest due row
// has waited past its due time. Claimed and staged rows do not count.
func TestCommPBacklog(t *testing.T) {
	p := newTestPipeline(t)
	*p.delay = time.Hour
	ctx := t.Context()

	b, err := p.commp.backlog(ctx)
	require.NoError(t, err)
	require.Equal(t, commpBacklog{}, b, "an empty pipeline has no backlog")

	insert := func(seed string, staged bool, commpTaskID any, age string) {
		blob, _, _ := testBlobAndPiece(t, seed)
		_, err := p.db.Exec(ctx, `
			INSERT INTO pdp_blob_pipeline (digest, staged, commp_task_id, created_at)
			VALUES ($1, $2, $3, now() - $4::interval)
		`, []byte(blob), staged, commpTaskID, age)
		require.NoError(t, err)
	}
	insert("held-young", false, nil, "10 minutes")
	insert("held-older", false, nil, "50 minutes")
	insert("due", false, nil, "3 hours")
	insert("due-recent", false, nil, "61 minutes")
	insert("claimed", false, 7, "4 hours")
	insert("staged", true, nil, "5 hours")

	b, err = p.commp.backlog(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, b.held, "rows inside the delay are held")
	require.EqualValues(t, 2, b.due, "unclaimed rows past the delay are due")
	require.InDelta(t, (2 * time.Hour).Seconds(), b.oldestDue.Seconds(), 60,
		"the oldest due row came due two hours ago")

	*p.delay = 0
	b, err = p.commp.backlog(ctx)
	require.NoError(t, err)
	require.Zero(t, b.held, "with no delay nothing is held")
	require.EqualValues(t, 4, b.due)
}

// TestDo_CancelledHeldRowIsNoop: a task that starts after the removal sweep
// cancelled its row (released during the delay) completes without work.
func TestDo_CancelledHeldRowIsNoop(t *testing.T) {
	p := newTestPipeline(t)
	done, err := p.commp.Do(999, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
}

// TestClaims_SkipPendingRemoval: a blob whose release has queued a removal
// is not claimed, by the bored claim or a direct spawn, however due its row
// is: the sweep decides whether the row is cancelled or, after a racing
// accept revived the blob, left for a later claim.
func TestClaims_SkipPendingRemoval(t *testing.T) {
	p := newTestPipeline(t)
	*p.delay = time.Hour
	ctx := t.Context()
	blob, _, _ := testBlobAndPiece(t, "blob-removal-pending")
	require.NoError(t, p.entry.Enqueue(ctx, blob))
	p.backdate(t, blob, 2*time.Hour)
	_, err := p.db.Exec(ctx, `INSERT INTO pdp_pending_piece_removals (digest) VALUES ($1)`, []byte(blob))
	require.NoError(t, err)

	p.bored(t)
	require.Nil(t, p.row(t, blob).CommPTaskID, "the bored claim leaves a blob with a removal pending")
	p.commp.spawn(ctx, blob)
	require.Nil(t, p.row(t, blob).CommPTaskID, "so does a direct spawn")

	// The sweep dropped the removal (a racing accept revived the blob).
	_, err = p.db.Exec(ctx, `DELETE FROM pdp_pending_piece_removals WHERE digest = $1`, []byte(blob))
	require.NoError(t, err)
	p.bored(t)
	require.NotNil(t, p.row(t, blob).CommPTaskID, "claimed once the removal is gone")
}
