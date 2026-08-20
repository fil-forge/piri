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

	"github.com/fil-forge/piri/pkg/pdp/promise"
	"github.com/fil-forge/piri/pkg/pdp/types"
)

const CommPTaskName = "PDPCommP"

// CommPTask computes a blob's commP and parks the piece, then hands the
// pipeline row to the aggregation stage. One task per pdp_blob_pipeline row,
// claimed via commp_task_id. Every step is idempotent — CalculateCommP
// dedups its mapping, parking is skipped when the piece already has refs,
// and the aggregation handoff dedups on agg_task_id — so a crashed task
// re-runs safely. A row deleted mid-run (removal-sweep cancel) makes the
// task complete as a noop.
type CommPTask struct {
	db  *harmonydb.DB
	api types.PieceAPI
	agg *AggregateTask

	add promise.Promise[harmonytask.AddTaskFunc]
}

func NewCommPTask(db *harmonydb.DB, api types.PieceAPI, agg *AggregateTask) *CommPTask {
	return &CommPTask{db: db, api: api, agg: agg}
}

// spawn creates a PDPCommP task claiming the blob's pipeline row. Best-effort
// (AddTask swallows errors); IAmBored scavenges unclaimed rows.
func (t *CommPTask) spawn(ctx context.Context, blob multihash.Multihash) {
	t.add.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE pdp_blob_pipeline SET commp_task_id = $1
			WHERE digest = $2 AND commp_task_id IS NULL
		`, id, []byte(blob))
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
		// Scavenge rows whose task spawn was lost (AddTask swallows errors,
		// or the process died between the Enqueue commit and the spawn).
		IAmBored: func(add harmonytask.AddTaskFunc) error {
			add(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
				n, err := tx.Exec(`
					UPDATE pdp_blob_pipeline SET commp_task_id = $1
					WHERE digest = (
						SELECT digest FROM pdp_blob_pipeline
						WHERE commp_task_id IS NULL AND aggregate_root IS NULL
						ORDER BY created_at LIMIT 1
					)
				`, id)
				return n > 0, err
			})
			return nil
		},
	}
}

func (t *CommPTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.add.Set(taskFunc)
}

var _ = harmonytask.Reg(&CommPTask{})
var _ harmonytask.TaskInterface = &CommPTask{}
