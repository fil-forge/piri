package pipeline

import (
	"context"
	"fmt"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/ipfs/go-cid"

	"github.com/fil-forge/piri/pkg/pdp/aggregation/manager"
)

// RootSubmitter buffers aggregate roots for on-chain submission, batching
// them into PDPAddRoots tasks. A batch is submitted as soon as BatchSize
// roots are buffered, and partial batches are flushed on the poll interval
// (via the PDPAddRoots task's IAmBored).
type RootSubmitter interface {
	Submit(ctx context.Context, roots ...cid.Cid) error
}

// SubmissionManager is the harmonydb-backed RootSubmitter. Buffered roots
// are pdp_root_submissions rows; a PDPAddRoots task claims a batch by
// stamping add_task_id.
type SubmissionManager struct {
	db       *harmonydb.DB
	cfg      manager.ConfigProvider
	addRoots *AddRootsTask
}

func NewSubmissionManager(db *harmonydb.DB, cfg manager.ConfigProvider, addRoots *AddRootsTask) *SubmissionManager {
	return &SubmissionManager{db: db, cfg: cfg, addRoots: addRoots}
}

var _ RootSubmitter = (*SubmissionManager)(nil)

func (m *SubmissionManager) Submit(ctx context.Context, roots ...cid.Cid) error {
	for _, root := range roots {
		if _, err := m.db.Exec(ctx, `
			INSERT INTO pdp_root_submissions (root) VALUES ($1)
			ON CONFLICT (root) DO NOTHING
		`, root.String()); err != nil {
			return fmt.Errorf("buffering root %s for submission: %w", root, err)
		}
		log.Infow("buffered aggregate root for submission", "root", root.String())
	}
	return m.flush(ctx, false)
}

// flush claims full batches of unclaimed roots into PDPAddRoots tasks; with
// force it also claims a trailing partial batch (the poll-interval flush).
// It first heals submission rows lost between an aggregation fold and its
// Submit call, deriving them from pipeline rows marked with an
// aggregate_root that was never staged.
func (m *SubmissionManager) flush(ctx context.Context, force bool) error {
	if _, err := m.db.Exec(ctx, `
		INSERT INTO pdp_root_submissions (root)
		SELECT DISTINCT aggregate_root FROM pdp_blob_pipeline
		WHERE aggregate_root IS NOT NULL
		ON CONFLICT (root) DO NOTHING
	`); err != nil {
		return fmt.Errorf("healing submission rows: %w", err)
	}

	batchSize := int(m.cfg.BatchSize())
	for {
		var unclaimed int
		if err := m.db.QueryRow(ctx, `
			SELECT count(*) FROM pdp_root_submissions WHERE add_task_id IS NULL
		`).Scan(&unclaimed); err != nil {
			return fmt.Errorf("counting unclaimed roots: %w", err)
		}
		if unclaimed == 0 || (!force && unclaimed < batchSize) {
			return nil
		}
		var claimed bool
		m.addRoots.add.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
			n, err := tx.Exec(`
				UPDATE pdp_root_submissions SET add_task_id = $1
				WHERE root IN (
					SELECT root FROM pdp_root_submissions
					WHERE add_task_id IS NULL
					ORDER BY created_at LIMIT $2
				)
			`, id, batchSize)
			claimed = n > 0
			return n > 0, err
		})
		if !claimed {
			// AddTask failed (error swallowed) or the rows were claimed
			// concurrently; the next Submit or IAmBored flush retries.
			return nil
		}
	}
}
