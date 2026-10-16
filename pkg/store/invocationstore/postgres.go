package invocationstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/fil-forge/ucantone/ucan"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	"github.com/fil-forge/piri/pkg/store"
)

// PostgresStore implements InvocationStore on the ucan_invocations table.
type PostgresStore struct {
	db *harmonydb.DB
}

var _ InvocationStore = (*PostgresStore)(nil)

// NewPostgresStore returns an InvocationStore on db.
func NewPostgresStore(db *harmonydb.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Get(ctx context.Context, link cid.Cid) (ucan.Invocation, error) {
	var data []byte
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		return tx.QueryRow(`SELECT bytes FROM ucan_invocations WHERE cid = $1`, link.String()).Scan(&data)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("getting invocation: %w", store.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting invocation: %w", err)
	}
	return Codec{}.Decode(data)
}

func (s *PostgresStore) Put(ctx context.Context, inv ucan.Invocation) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO ucan_invocations (cid, bytes) VALUES ($1, $2)
			ON CONFLICT (cid) DO UPDATE SET bytes = EXCLUDED.bytes`,
			inv.Link().String(), inv.Bytes())
		return err
	})
	if err != nil {
		return fmt.Errorf("putting invocation: %w", err)
	}
	return nil
}

func (s *PostgresStore) Delete(ctx context.Context, link cid.Cid) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`DELETE FROM ucan_invocations WHERE cid = $1`, link.String())
		return err
	})
	if err != nil {
		return fmt.Errorf("deleting invocation: %w", err)
	}
	return nil
}
