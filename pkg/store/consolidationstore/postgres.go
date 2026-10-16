package consolidationstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/consolidationstore/consolidation"
)

// PostgresStore implements Store on the ucan_consolidations table.
type PostgresStore struct {
	db *harmonydb.DB
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore returns a consolidation Store on db.
func NewPostgresStore(db *harmonydb.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Get(ctx context.Context, batchCID cid.Cid) (consolidation.Consolidation, error) {
	var (
		track       []byte
		consolidate string
	)
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		return tx.QueryRow(`
			SELECT track_invocation, consolidate_invocation
			FROM ucan_consolidations WHERE batch = $1`,
			batchCID.String()).Scan(&track, &consolidate)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return consolidation.Consolidation{}, store.ErrNotFound
	}
	if err != nil {
		return consolidation.Consolidation{}, fmt.Errorf("getting consolidation: %w", err)
	}
	link, err := cid.Parse(consolidate)
	if err != nil {
		return consolidation.Consolidation{}, fmt.Errorf("parsing consolidate invocation: %w", err)
	}
	return consolidation.Consolidation{TrackInvocationBytes: track, ConsolidateInvocationCID: link}, nil
}

func (s *PostgresStore) Put(ctx context.Context, batchCID cid.Cid, c consolidation.Consolidation) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO ucan_consolidations (batch, track_invocation, consolidate_invocation)
			VALUES ($1, $2, $3)
			ON CONFLICT (batch) DO UPDATE SET
				track_invocation = EXCLUDED.track_invocation,
				consolidate_invocation = EXCLUDED.consolidate_invocation`,
			batchCID.String(), c.TrackInvocationBytes, c.ConsolidateInvocationCID.String())
		return err
	})
	if err != nil {
		return fmt.Errorf("putting consolidation: %w", err)
	}
	return nil
}

func (s *PostgresStore) Delete(ctx context.Context, batchCID cid.Cid) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`DELETE FROM ucan_consolidations WHERE batch = $1`, batchCID.String())
		return err
	})
	if err != nil {
		return fmt.Errorf("deleting consolidation: %w", err)
	}
	return nil
}
