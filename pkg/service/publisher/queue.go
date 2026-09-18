package publisher

import (
	"context"
	"fmt"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/ipfs/go-cid"
)

// AdvertQueue holds the location commitments whose IPNI advertisement is
// still owed. /blob/accept enqueues a claim instead of publishing inline, and
// /blob/release dequeues one whose blob went away before it was published.
// The IPNIPublish task drains the queue in batches.
type AdvertQueue interface {
	Enqueue(ctx context.Context, claim cid.Cid) error
	Dequeue(ctx context.Context, claim cid.Cid) error
}

// DBQueue is the AdvertQueue on harmonydb (ipni_pending_adverts), the same
// database the PDP pipeline runs on, so a task claims and retires rows the
// way the pipeline's tasks do.
type DBQueue struct {
	db *harmonydb.DB
}

func NewDBQueue(db *harmonydb.DB) *DBQueue { return &DBQueue{db: db} }

// Enqueue records that the claim's advertisement is owed. A claim already
// queued is left as it is.
func (q *DBQueue) Enqueue(ctx context.Context, claim cid.Cid) error {
	_, err := q.db.Exec(ctx, `
		INSERT INTO ipni_pending_adverts (claim) VALUES ($1)
		ON CONFLICT (claim) DO NOTHING
	`, claim.String())
	if err != nil {
		return fmt.Errorf("queueing advertisement for %s: %w", claim, err)
	}
	return nil
}

// Dequeue drops the owed advertisement for the claim, whether or not one is
// queued. A row a task has already claimed goes too; the task then finds the
// claim gone from the store and skips it.
func (q *DBQueue) Dequeue(ctx context.Context, claim cid.Cid) error {
	_, err := q.db.Exec(ctx, `DELETE FROM ipni_pending_adverts WHERE claim = $1`, claim.String())
	if err != nil {
		return fmt.Errorf("dequeueing advertisement for %s: %w", claim, err)
	}
	return nil
}

// claimBatch stamps up to limit unclaimed rows, oldest first, with the task
// id, and reports whether any were.
func claimBatch(tx *harmonydb.Tx, id harmonytask.TaskID, limit int) (bool, error) {
	n, err := tx.Exec(`
		UPDATE ipni_pending_adverts SET publish_task_id = $1
		WHERE claim IN (
			SELECT claim FROM ipni_pending_adverts
			WHERE publish_task_id IS NULL
			ORDER BY created_at LIMIT $2
		)
	`, id, limit)
	return n > 0, err
}

// claimed returns the claims stamped with the task id, oldest first.
func (q *DBQueue) claimed(ctx context.Context, id harmonytask.TaskID) ([]cid.Cid, error) {
	var rows []struct {
		Claim string `db:"claim"`
	}
	if err := q.db.Select(ctx, &rows, `
		SELECT claim FROM ipni_pending_adverts WHERE publish_task_id = $1 ORDER BY created_at
	`, id); err != nil {
		return nil, fmt.Errorf("loading claimed advertisements: %w", err)
	}
	claims := make([]cid.Cid, len(rows))
	for i, r := range rows {
		c, err := cid.Parse(r.Claim)
		if err != nil {
			return nil, fmt.Errorf("parsing queued claim %q: %w", r.Claim, err)
		}
		claims[i] = c
	}
	return claims, nil
}

// retire deletes the rows stamped with the task id.
func (q *DBQueue) retire(ctx context.Context, id harmonytask.TaskID) error {
	_, err := q.db.Exec(ctx, `DELETE FROM ipni_pending_adverts WHERE publish_task_id = $1`, id)
	if err != nil {
		return fmt.Errorf("retiring published advertisements: %w", err)
	}
	return nil
}

// reclaimOrphans returns rows to the unclaimed pool whose task the engine no
// longer has: harmonytask deletes a task after it fails too many times, and
// its rows would otherwise stay stamped with an id nothing will run again.
// It reports how many were reclaimed.
func (q *DBQueue) reclaimOrphans(ctx context.Context) (int, error) {
	n, err := q.db.Exec(ctx, `
		UPDATE ipni_pending_adverts SET publish_task_id = NULL
		WHERE publish_task_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM harmony_task WHERE id = ipni_pending_adverts.publish_task_id)
	`)
	if err != nil {
		return 0, fmt.Errorf("reclaiming orphaned advertisements: %w", err)
	}
	return n, nil
}

// pending reports how many advertisements are queued and not yet published,
// claimed by a task or not, and how long the oldest has waited. A growing age
// means the publish task is not keeping up, or is failing.
func (q *DBQueue) pending(ctx context.Context) (count int64, oldest time.Duration, err error) {
	var rows []struct {
		N   int64   `db:"n"`
		Age float64 `db:"age"`
	}
	if err := q.db.Select(ctx, &rows, `
		SELECT count(*) AS n,
		       coalesce(extract(epoch FROM now() - min(created_at)), 0)::double precision AS age
		FROM ipni_pending_adverts
	`); err != nil {
		return 0, 0, fmt.Errorf("measuring pending advertisements: %w", err)
	}
	if len(rows) == 0 {
		return 0, 0, nil
	}
	return rows[0].N, time.Duration(rows[0].Age * float64(time.Second)), nil
}
