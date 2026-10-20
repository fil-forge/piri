package pipeline

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
	"github.com/fil-forge/piri/pkg/pdp/promise"
	"github.com/fil-forge/piri/pkg/pdp/types"
)

const CommPTaskName = "PDPCommP"

// boredClaimLimit caps the due rows one IAmBored call claims. With a delay
// set, IAmBored is how every row reaches its commP task, so it claims as many
// as are due rather than one; the cap bounds a single call when a backlog has
// built up. Tasks past the handler's Max sit unowned in harmony_task for the
// poller, on this node or another.
const boredClaimLimit = 256

// CommPTask computes a blob's commP and parks the piece, then hands the
// pipeline row to the aggregation stage. One task per pdp_blob_pipeline row,
// claimed via commp_task_id. Every step is idempotent — CalculateCommP
// dedups its mapping, parking is skipped when the piece already has refs,
// and the aggregation handoff dedups on agg_task_id — so a crashed task
// re-runs safely. A row whose blob is staged reaches this stage only once
// the settle task has moved the blob to its digest's key. The removal sweep
// cancels a row only while no live task holds it, so a task never records commp for a removed blob; a row cancelled
// before the task starts makes it complete as a noop.
//
// A row is due once the policy's delay has passed since it was created, and
// only a due row is claimed: by the spawn at acceptance when there is no
// delay, otherwise by IAmBored on the engine's poll. A row released before it
// is due is cancelled by the removal sweep and never costs a commP.
type CommPTask struct {
	db     *harmonydb.DB
	api    types.PieceAPI
	agg    *AggregateTask
	policy commp.Policy

	add promise.Promise[harmonytask.AddTaskFunc]
}

func NewCommPTask(db *harmonydb.DB, api types.PieceAPI, agg *AggregateTask, policy commp.Policy) *CommPTask {
	return &CommPTask{db: db, api: api, agg: agg, policy: policy}
}

// delaySeconds is the current delay as the seconds argument of the claim
// queries' make_interval.
func (t *CommPTask) delaySeconds() float64 {
	return t.policy.Delay().Seconds()
}

// spawn creates a PDPCommP task claiming the blob's pipeline row, if the row
// is due. Best-effort (AddTask swallows errors); IAmBored claims rows whose
// spawn was lost or that were not due yet.
func (t *CommPTask) spawn(ctx context.Context, blob multihash.Multihash) {
	t.add.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE pdp_blob_pipeline SET commp_task_id = $1
			WHERE digest = $2 AND commp_task_id IS NULL AND NOT staged
			  AND created_at <= now() - make_interval(secs => $3::double precision)
		`, id, []byte(blob), t.delaySeconds())
		return n > 0, err
	})
}

func (t *CommPTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var rows []struct {
		Blob  []byte  `db:"digest"`
		Commp *string `db:"commp"`
	}
	if err := t.db.Select(ctx, &rows, `
		SELECT digest, commp FROM pdp_blob_pipeline WHERE commp_task_id = $1
	`, taskID); err != nil {
		return false, fmt.Errorf("loading pipeline row: %w", err)
	}
	if len(rows) == 0 {
		// Row cancelled by the removal sweep (or never claimed): nothing to do.
		return true, nil
	}
	// A commp_task_id claims exactly one row: digest is the table's primary
	// key and every claim UPDATE (spawn, IAmBored scavenge) keys on a single
	// digest. rows[0] below relies on this — a multi-row claim would strand
	// the extras, since a claimed commp_task_id is never cleared.
	blob := multihash.Multihash(rows[0].Blob)

	if rows[0].Commp == nil {
		res, err := t.api.CalculateCommP(ctx, blob)
		if err != nil {
			return false, fmt.Errorf("calculating commp for %s: %w", blob.String(), err)
		}
		log.Infow("calculated commp", "blob", blob.String(), "piece", res.PieceCID.String())

		// Park once: a retried task (or a revived blob whose bytes are still
		// parked) must not double-insert the parked-piece chain.
		var parked bool
		if err := t.db.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM pdp_piecerefs WHERE piece_cid = $1)
		`, res.PieceCID.String()).Scan(&parked); err != nil {
			return false, fmt.Errorf("checking piece refs: %w", err)
		}
		if !parked {
			if err := t.api.ParkPiece(ctx, types.ParkPieceRequest{
				Blob:       blob,
				PieceCID:   res.PieceCID,
				RawSize:    res.RawSize,
				PaddedSize: res.PaddedSize,
			}); err != nil {
				return false, fmt.Errorf("parking piece: %w", err)
			}
		}

		if _, err := t.db.Exec(ctx, `
			UPDATE pdp_blob_pipeline SET commp = $1, commp_hashed_at = now() WHERE digest = $2
		`, res.PieceCID.String(), []byte(blob)); err != nil {
			return false, fmt.Errorf("recording commp: %w", err)
		}
	}

	// Handoff: spawn the aggregation task for this piece. Dedup on
	// agg_task_id; if the row vanished (cancelled) this is a rollback-noop.
	t.agg.spawn(ctx, blob)
	return true, nil
}

func (t *CommPTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *CommPTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(runtime.NumCPU()),
		Name: CommPTaskName,
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 256 << 20,
		},
		MaxFailures: 50,
		RetryWait:   taskhelp.RetryWaitLinear(5*time.Second, 5*time.Second),
		// Claim due rows that have no task: rows held back by the delay, and
		// rows whose spawn was lost (AddTask swallows errors, or the process
		// died between the Enqueue commit and the spawn). The engine calls
		// this on each poll in which it found no other work and this handler
		// has capacity, so a row is claimed within a poll of coming due.
		IAmBored: func(add harmonytask.AddTaskFunc) error {
			for range boredClaimLimit {
				claimed := false
				add(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
					n, err := tx.Exec(`
						UPDATE pdp_blob_pipeline SET commp_task_id = $1
						WHERE commp_task_id IS NULL AND aggregate_root IS NULL AND NOT staged AND digest = (
							SELECT digest FROM pdp_blob_pipeline
							WHERE commp_task_id IS NULL AND aggregate_root IS NULL AND NOT staged
							  AND created_at <= now() - make_interval(secs => $2::double precision)
							ORDER BY created_at LIMIT 1
						)
					`, id, t.delaySeconds())
					claimed = n > 0
					return claimed, err
				})
				if !claimed {
					break
				}
			}
			return nil
		},
	}
}

func (t *CommPTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.add.Set(taskFunc)
}

var _ = harmonytask.Reg(&CommPTask{})
var _ harmonytask.TaskInterface = &CommPTask{}
