package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/piri/pkg/pdp/promise"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

const SettleTaskName = "PDPSettle"

// settleConcurrency bounds the settle tasks one node runs at once. A settle is
// a rename (flatfs) or a copy inside the object store (MinIO), so it costs
// little of the node's own CPU or memory, and more of them run at once than
// commP tasks do.
const settleConcurrency = 16

// Settler moves a staged blob to the key of its digest.
type Settler interface {
	Settle(ctx context.Context, digest multihash.Multihash) error
}

var _ Settler = (*blobstore.StagingStore)(nil)

// SettleTask moves an accepted blob that is still staged to the key of its
// digest, then hands its pipeline row to the commP stage. It runs apart from
// commP so a settle never waits behind commP's long reads or its concurrency
// limit. One task per staged pdp_blob_pipeline row, claimed via
// settle_task_id. Settling is idempotent, so a crashed task re-runs safely. A
// row cancelled by the removal sweep before the task runs makes it a noop; a
// removal while it runs waits for the move in the staging store, then removes
// the blob from wherever the move left it.
type SettleTask struct {
	db      *harmonydb.DB
	settler Settler
	commp   *CommPTask

	add promise.Promise[harmonytask.AddTaskFunc]
}

func NewSettleTask(db *harmonydb.DB, settler Settler, commp *CommPTask) *SettleTask {
	return &SettleTask{db: db, settler: settler, commp: commp}
}

// spawn creates a PDPSettle task claiming the blob's pipeline row. Best-effort
// (AddTask swallows errors); IAmBored scavenges unclaimed rows.
func (t *SettleTask) spawn(ctx context.Context, blob multihash.Multihash) {
	t.add.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE pdp_blob_pipeline SET settle_task_id = $1
			WHERE digest = $2 AND staged AND settle_task_id IS NULL
		`, id, []byte(blob))
		return n > 0, err
	})
}

func (t *SettleTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var rows []struct {
		Blob []byte `db:"digest"`
	}
	if err := t.db.Select(ctx, &rows, `
		SELECT digest FROM pdp_blob_pipeline WHERE settle_task_id = $1
	`, taskID); err != nil {
		return false, fmt.Errorf("loading pipeline row: %w", err)
	}
	if len(rows) == 0 {
		// Row cancelled by the removal sweep (or never claimed): nothing to do.
		return true, nil
	}
	// A settle_task_id claims exactly one row, as a commp_task_id does.
	blob := multihash.Multihash(rows[0].Blob)

	if err := t.settler.Settle(ctx, blob); err != nil {
		return false, fmt.Errorf("settling %s: %w", blob.String(), err)
	}
	log.Infow("settled staged blob", "blob", blob.String())

	if _, err := t.db.Exec(ctx, `
		UPDATE pdp_blob_pipeline SET staged = false WHERE digest = $1
	`, []byte(blob)); err != nil {
		return false, fmt.Errorf("recording settle: %w", err)
	}

	// Handoff: spawn the commP task for this blob. Dedup on commp_task_id; a
	// lost spawn is scavenged by the commP task's IAmBored.
	t.commp.spawn(ctx, blob)
	return true, nil
}

func (t *SettleTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *SettleTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(settleConcurrency),
		Name: SettleTaskName,
		Cost: resources.Resources{
			Cpu: 0,
			Ram: 32 << 20,
		},
		MaxFailures: 50,
		RetryWait:   taskhelp.RetryWaitLinear(5*time.Second, 5*time.Second),
		// Scavenge staged rows whose task spawn was lost. The outer WHERE
		// repeats the claim condition: a row another scavenger claimed after
		// this one selected it is left to that one.
		IAmBored: func(add harmonytask.AddTaskFunc) error {
			add(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
				n, err := tx.Exec(`
					UPDATE pdp_blob_pipeline SET settle_task_id = $1
					WHERE staged AND settle_task_id IS NULL AND digest = (
						SELECT digest FROM pdp_blob_pipeline
						WHERE staged AND settle_task_id IS NULL
						ORDER BY created_at LIMIT 1
					)
				`, id)
				return n > 0, err
			})
			return nil
		},
	}
}

func (t *SettleTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.add.Set(taskFunc)
}

var _ = harmonytask.Reg(&SettleTask{})
var _ harmonytask.TaskInterface = &SettleTask{}
