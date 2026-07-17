package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/ipfs/go-cid"

	"github.com/fil-forge/piri/pkg/pdp/aggregation/manager"
	aggtypes "github.com/fil-forge/piri/pkg/pdp/aggregation/types"
	"github.com/fil-forge/piri/pkg/pdp/promise"
	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
)

const AddRootsTaskName = "PDPAddRoots"

// AddRootsTask submits a claimed batch of aggregate roots on-chain: it
// issues the piece-accept receipts, calls AddRoots (idempotent per root —
// a retried batch skips roots already staged), and retires the batch's
// pipeline and submission rows. Its IAmBored is the poll-interval flush of
// partial batches, replacing the previous manager's process loop.
type AddRootsTask struct {
	db       *harmonydb.DB
	api      pdptypes.ProofSetAPI
	proofSet pdptypes.ProofSetIDProvider
	store    aggtypes.Store
	accepter *manager.PieceAcceptor
	cfg      manager.ConfigProvider

	add promise.Promise[harmonytask.AddTaskFunc]

	boredMu   sync.Mutex
	lastBored time.Time
}

func NewAddRootsTask(
	db *harmonydb.DB,
	api pdptypes.ProofSetAPI,
	proofSet pdptypes.ProofSetIDProvider,
	store aggtypes.Store,
	accepter *manager.PieceAcceptor,
	cfg manager.ConfigProvider,
) *AddRootsTask {
	return &AddRootsTask{db: db, api: api, proofSet: proofSet, store: store, accepter: accepter, cfg: cfg}
}

func (t *AddRootsTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var rows []struct {
		Root string `db:"root"`
	}
	if err := t.db.Select(ctx, &rows, `
		SELECT root FROM pdp_root_submissions WHERE add_task_id = $1
	`, taskID); err != nil {
		return false, fmt.Errorf("loading claimed roots: %w", err)
	}
	if len(rows) == 0 {
		return true, nil
	}

	links := make([]cid.Cid, len(rows))
	rootStrs := make([]string, len(rows))
	for i, r := range rows {
		c, err := cid.Parse(r.Root)
		if err != nil {
			return false, fmt.Errorf("parsing root cid %s: %w", r.Root, err)
		}
		links[i] = c
		rootStrs[i] = r.Root
	}

	if err := t.accepter.AcceptPieces(ctx, links); err != nil {
		return false, fmt.Errorf("issuing piece-accept receipts: %w", err)
	}

	proofSetID, err := t.proofSet.ProofSetID(ctx)
	if err != nil {
		return false, fmt.Errorf("getting proof set ID: %w", err)
	}

	roots := make([]pdptypes.RootAdd, len(links))
	for i, link := range links {
		a, err := t.store.Get(ctx, link)
		if err != nil {
			return false, fmt.Errorf("reading aggregate %s: %w", link, err)
		}
		subRoots := make([]cid.Cid, len(a.Pieces))
		for j, p := range a.Pieces {
			subRoots[j] = p.Link
		}
		roots[i] = pdptypes.RootAdd{Root: a.Root, SubRoots: subRoots}
	}

	txHash, err := t.api.AddRoots(ctx, proofSetID, roots)
	if err != nil {
		return false, fmt.Errorf("adding roots: %w", err)
	}
	log.Infow("added roots", "count", len(roots), "tx", txHash)

	// Retire the batch: staged pieces are tracked in the pdpv0 tables from
	// here on, so the pipeline rows (and the submission buffer rows) are done.
	if _, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		if _, err := tx.Exec(`
			DELETE FROM pdp_blob_pipeline WHERE aggregate_root = ANY($1)
		`, rootStrs); err != nil {
			return false, fmt.Errorf("retiring pipeline rows: %w", err)
		}
		if _, err := tx.Exec(`
			DELETE FROM pdp_root_submissions WHERE add_task_id = $1
		`, taskID); err != nil {
			return false, fmt.Errorf("retiring submission rows: %w", err)
		}
		return true, nil
	}, harmonydb.OptionRetry()); err != nil {
		return false, err
	}
	return true, nil
}

func (t *AddRootsTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *AddRootsTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(3), // matches the previous manager queue's 3 workers
		Name: AddRootsTaskName,
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
		},
		MaxFailures: 50,
		RetryWait:   taskhelp.RetryWaitLinear(10*time.Second, 10*time.Second),
		// The poll-interval flush: claim any buffered roots (partial batch
		// included) once per poll interval, like the old manager loop. Rate
		// is interval-guarded because IAmBored fires on every idle scheduler
		// pass; a pass with nothing to claim rolls back without churn.
		IAmBored: func(add harmonytask.AddTaskFunc) error {
			t.boredMu.Lock()
			if time.Since(t.lastBored) < t.cfg.PollInterval() {
				t.boredMu.Unlock()
				return nil
			}
			t.lastBored = time.Now()
			t.boredMu.Unlock()

			// Heal submission rows lost between a fold and its Submit call.
			if _, err := t.db.Exec(context.Background(), `
				INSERT INTO pdp_root_submissions (root)
				SELECT DISTINCT aggregate_root FROM pdp_blob_pipeline
				WHERE aggregate_root IS NOT NULL
				ON CONFLICT (root) DO NOTHING
			`); err != nil {
				return fmt.Errorf("healing submission rows: %w", err)
			}

			batchSize := int(t.cfg.BatchSize())
			add(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
				n, err := tx.Exec(`
					UPDATE pdp_root_submissions SET add_task_id = $1
					WHERE root IN (
						SELECT root FROM pdp_root_submissions
						WHERE add_task_id IS NULL
						ORDER BY created_at LIMIT $2
					)
				`, id, batchSize)
				return n > 0, err
			})
			return nil
		},
	}
}

func (t *AddRootsTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.add.Set(taskFunc)
}

var _ = harmonytask.Reg(&AddRootsTask{})
var _ harmonytask.TaskInterface = &AddRootsTask{}
