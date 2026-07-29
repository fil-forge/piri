package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"

	libpiece "github.com/fil-forge/libforge/piece"

	"github.com/fil-forge/piri/pkg/pdp/aggregation/aggregator"
	aggtypes "github.com/fil-forge/piri/pkg/pdp/aggregation/types"
	"github.com/fil-forge/piri/pkg/pdp/promise"
)

const AggregateTaskName = "PDPAggregate"

// AggregateTask folds commP'd pipeline rows into aggregates. One task is
// spawned per piece (by the commp task's handoff, deduped on agg_task_id),
// but each run folds EVERY unaggregated row — the buffer is derived from
// pdp_blob_pipeline rather than persisted, and Max=1 serializes folds so
// two tasks never aggregate the same rows. Rows below the aggregation
// threshold stay buffered (aggregate_root NULL) until later pieces push
// them over.
//
// The fold runs in a single transaction with the candidate rows locked
// (FOR UPDATE) so the removal sweep's cancel — DELETE WHERE aggregate_root
// IS NULL — serializes against it: a row is either cancelled before the
// fold reads it, or folded and no longer cancellable. Without the lock a
// row could be deleted after the fold read it, producing an aggregate that
// proves deleted bytes.
type AggregateTask struct {
	db        *harmonydb.DB
	store     aggtypes.Store
	submitter RootSubmitter

	add promise.Promise[harmonytask.AddTaskFunc]
}

func NewAggregateTask(db *harmonydb.DB, store aggtypes.Store, submitter RootSubmitter) *AggregateTask {
	return &AggregateTask{db: db, store: store, submitter: submitter}
}

// spawn creates a PDPAggregate task for the blob's piece, deduped on
// agg_task_id. Best-effort; Follows and IAmBored recover lost spawns.
func (t *AggregateTask) spawn(ctx context.Context, blob multihash.Multihash) {
	t.add.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE pdp_blob_pipeline SET agg_task_id = $1
			WHERE digest = $2 AND agg_task_id IS NULL AND aggregate_root IS NULL
		`, id, []byte(blob))
		return n > 0, err
	})
}

func (t *AggregateTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var roots []cid.Cid
	if _, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		roots = roots[:0]

		var rows []struct {
			Blob  []byte `db:"digest"`
			Commp string `db:"commp"`
		}
		if err := tx.Select(&rows, `
			SELECT digest, commp FROM pdp_blob_pipeline
			WHERE commp IS NOT NULL AND aggregate_root IS NULL
			ORDER BY created_at, digest
			FOR UPDATE
		`); err != nil {
			return false, fmt.Errorf("loading unaggregated pieces: %w", err)
		}
		if len(rows) == 0 {
			return false, nil
		}

		pieces := make([]libpiece.Piece, 0, len(rows))
		for _, r := range rows {
			c, err := cid.Parse(r.Commp)
			if err != nil {
				return false, fmt.Errorf("parsing piece cid %s: %w", r.Commp, err)
			}
			p, err := libpiece.FromCID(c)
			if err != nil {
				return false, fmt.Errorf("decoding piece %s: %w", r.Commp, err)
			}
			pieces = append(pieces, p)
		}

		aggregates, err := aggregator.Append(pieces)
		if err != nil {
			return false, fmt.Errorf("folding aggregates: %w", err)
		}
		if len(aggregates) == 0 {
			// Below threshold: rows stay buffered until more pieces arrive.
			return false, nil
		}

		for _, a := range aggregates {
			// Persisting into the aggregate store inside the transaction is
			// safe: Put is idempotent, and an orphan write from an aborted
			// transaction is unreferenced.
			if err := t.store.Put(ctx, a.Root, a); err != nil {
				return false, fmt.Errorf("storing aggregate %s: %w", a.Root, err)
			}
			members := make([]string, len(a.Pieces))
			for i, p := range a.Pieces {
				members[i] = p.Link.String()
			}
			if _, err := tx.Exec(`
				UPDATE pdp_blob_pipeline SET aggregate_root = $1, aggregated_at = now()
				WHERE commp = ANY($2) AND aggregate_root IS NULL
			`, a.Root.String(), members); err != nil {
				return false, fmt.Errorf("marking aggregated pieces: %w", err)
			}
			roots = append(roots, a.Root)
			log.Infow("aggregate created", "root", a.Root.String(), "pieces", len(a.Pieces))
		}
		return true, nil
	}, harmonydb.OptionRetry()); err != nil {
		return false, err
	}

	if len(roots) > 0 {
		// Submission-row insert is idempotent and the manager heals missing
		// rows from aggregate_root marks, so a crash here loses nothing.
		if err := t.submitter.Submit(ctx, roots...); err != nil {
			return false, fmt.Errorf("submitting aggregates: %w", err)
		}
	}
	return true, nil
}

// followCommP is the Follows crash net: when a PDPCommP task completes but
// its aggregation handoff was lost, the follow poller (~1 min cadence)
// spawns the missing PDPAggregate task. Dedup is the engine's previous_task
// check plus the agg_task_id guard.
func (t *AggregateTask) followCommP(commpTaskID harmonytask.TaskID, add harmonytask.AddTaskFunc) (bool, error) {
	var missing bool
	if err := t.db.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM pdp_blob_pipeline
			WHERE commp_task_id = $1 AND commp IS NOT NULL
			  AND aggregate_root IS NULL AND agg_task_id IS NULL
		)`, commpTaskID).Scan(&missing); err != nil {
		return false, err
	}
	if !missing {
		return false, nil
	}
	add(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE pdp_blob_pipeline SET agg_task_id = $1
			WHERE commp_task_id = $2 AND agg_task_id IS NULL AND aggregate_root IS NULL
		`, id, commpTaskID)
		return n > 0, err
	})
	return true, nil
}

func (t *AggregateTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *AggregateTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1), // serialize folds — the derived buffer requires it
		Name: AggregateTaskName,
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
		},
		MaxFailures: 50,
		RetryWait:   taskhelp.RetryWaitLinear(5*time.Second, 5*time.Second),
		Follows: map[string]func(harmonytask.TaskID, harmonytask.AddTaskFunc) (bool, error){
			CommPTaskName: t.followCommP,
		},
		// Scavenge pieces whose aggregation spawn was lost outside the
		// Follows window (e.g. across an engine restart).
		IAmBored: func(add harmonytask.AddTaskFunc) error {
			add(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
				n, err := tx.Exec(`
					UPDATE pdp_blob_pipeline SET agg_task_id = $1
					WHERE digest = (
						SELECT digest FROM pdp_blob_pipeline
						WHERE commp IS NOT NULL AND aggregate_root IS NULL AND agg_task_id IS NULL
						ORDER BY created_at LIMIT 1
					)
				`, id)
				return n > 0, err
			})
			return nil
		},
	}
}

func (t *AggregateTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.add.Set(taskFunc)
}

var _ = harmonytask.Reg(&AggregateTask{})
var _ harmonytask.TaskInterface = &AggregateTask{}
