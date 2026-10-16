package receiptstore

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

// PostgresStore implements ReceiptStore on the ucan_receipts table.
type PostgresStore struct {
	db *harmonydb.DB
}

var _ ReceiptStore = (*PostgresStore)(nil)

// NewPostgresStore returns a ReceiptStore on db.
func NewPostgresStore(db *harmonydb.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Get(ctx context.Context, link cid.Cid) (ucan.Receipt, error) {
	var data []byte
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		return tx.QueryRow(`SELECT bytes FROM ucan_receipts WHERE cid = $1`, link.String()).Scan(&data)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("getting receipt: %w", store.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting receipt: %w", err)
	}
	return Codec{}.Decode(data)
}

func (s *PostgresStore) GetByRan(ctx context.Context, ran cid.Cid) (ucan.Receipt, error) {
	var data []byte
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		return tx.QueryRow(`SELECT bytes FROM ucan_receipts WHERE ran = $1`, ran.String()).Scan(&data)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("getting receipt by ran: %w", store.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting receipt by ran: %w", err)
	}
	return Codec{}.Decode(data)
}

// Put replaces any receipt stored for the same task, so the task finds the
// latest.
func (s *PostgresStore) Put(ctx context.Context, rcpt ucan.Receipt) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO ucan_receipts (cid, ran, bytes) VALUES ($1, $2, $3)
			ON CONFLICT (ran) DO UPDATE SET cid = EXCLUDED.cid, bytes = EXCLUDED.bytes`,
			rcpt.Link().String(), rcpt.Ran().String(), rcpt.Bytes())
		return err
	})
	if err != nil {
		return fmt.Errorf("putting receipt: %w", err)
	}
	return nil
}
