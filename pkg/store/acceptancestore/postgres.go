package acceptancestore

import (
	"context"
	"errors"
	"fmt"

	"github.com/fil-forge/ucantone/did"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/pgx/v5"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
)

// PostgresStore implements AcceptanceStore on the ucan_acceptances table.
type PostgresStore struct {
	db *harmonydb.DB
}

var _ AcceptanceStore = (*PostgresStore)(nil)

// NewPostgresStore returns an AcceptanceStore on db.
func NewPostgresStore(db *harmonydb.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Get(ctx context.Context, digest multihash.Multihash, space did.DID) (acc acceptance.Acceptance, err error) {
	err = harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		acc, err = scanAcceptance(tx.QueryRow(`
			SELECT digest, space, size, pdp_accept, executed_at, cause, site, allocation
			FROM ucan_acceptances WHERE digest = $1 AND space = $2`,
			[]byte(digest), space.String()))
		return err
	})
	if err != nil {
		return acceptance.Acceptance{}, fmt.Errorf("getting acceptance: %w", err)
	}
	return acc, nil
}

func (s *PostgresStore) GetAny(ctx context.Context, digest multihash.Multihash) (acc acceptance.Acceptance, err error) {
	err = harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		acc, err = scanAcceptance(tx.QueryRow(`
			SELECT digest, space, size, pdp_accept, executed_at, cause, site, allocation
			FROM ucan_acceptances WHERE digest = $1 LIMIT 1`,
			[]byte(digest)))
		return err
	})
	if err != nil {
		return acceptance.Acceptance{}, fmt.Errorf("getting any acceptance: %w", err)
	}
	return acc, nil
}

func (s *PostgresStore) Exists(ctx context.Context, digest multihash.Multihash) (exists bool, err error) {
	err = harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		return tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM ucan_acceptances WHERE digest = $1)`,
			[]byte(digest)).Scan(&exists)
	})
	if err != nil {
		return false, fmt.Errorf("checking acceptance exists: %w", err)
	}
	return exists, nil
}

func (s *PostgresStore) Put(ctx context.Context, acc acceptance.Acceptance) error {
	var allocation *string
	if acc.Allocation != nil {
		link := acc.Allocation.String()
		allocation = &link
	}
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO ucan_acceptances
				(digest, space, size, pdp_accept, executed_at, cause, site, allocation)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (digest, space) DO UPDATE SET
				size = EXCLUDED.size, pdp_accept = EXCLUDED.pdp_accept,
				executed_at = EXCLUDED.executed_at, cause = EXCLUDED.cause,
				site = EXCLUDED.site, allocation = EXCLUDED.allocation`,
			[]byte(acc.Blob.Digest), acc.Space.String(), int64(acc.Blob.Size),
			acc.PDPAccept.Task.String(), int64(acc.ExecutedAt), acc.Cause.String(),
			acc.Site.String(), allocation)
		return err
	})
	if err != nil {
		return fmt.Errorf("putting acceptance: %w", err)
	}
	return nil
}

func (s *PostgresStore) Delete(ctx context.Context, digest multihash.Multihash, space did.DID) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`DELETE FROM ucan_acceptances WHERE digest = $1 AND space = $2`,
			[]byte(digest), space.String())
		return err
	})
	if err != nil {
		return fmt.Errorf("deleting acceptance: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListSpaces(ctx context.Context, digest multihash.Multihash) (spaces []did.DID, err error) {
	err = harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		rows, err := tx.Query(`SELECT space FROM ucan_acceptances WHERE digest = $1`, []byte(digest))
		if err != nil {
			return err
		}
		spaces, err = store.ScanSpaces(rows)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("listing acceptances: %w", err)
	}
	return spaces, nil
}

func scanAcceptance(r store.Row) (acceptance.Acceptance, error) {
	var (
		digest                        []byte
		space, pdpAccept, cause, site string
		size, executedAt              int64
		allocation                    *string
	)
	if err := r.Scan(&digest, &space, &size, &pdpAccept, &executedAt, &cause, &site, &allocation); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return acceptance.Acceptance{}, store.ErrNotFound
		}
		return acceptance.Acceptance{}, err
	}
	acc := acceptance.Acceptance{
		Blob:       acceptance.Blob{Digest: digest, Size: uint64(size)},
		ExecutedAt: uint64(executedAt),
	}
	var err error
	if acc.Space, err = did.Parse(space); err != nil {
		return acceptance.Acceptance{}, fmt.Errorf("parsing space: %w", err)
	}
	if acc.PDPAccept.Task, err = cid.Parse(pdpAccept); err != nil {
		return acceptance.Acceptance{}, fmt.Errorf("parsing pdp accept task: %w", err)
	}
	if acc.Cause, err = cid.Parse(cause); err != nil {
		return acceptance.Acceptance{}, fmt.Errorf("parsing cause: %w", err)
	}
	if acc.Site, err = cid.Parse(site); err != nil {
		return acceptance.Acceptance{}, fmt.Errorf("parsing site: %w", err)
	}
	if allocation != nil {
		link, err := cid.Parse(*allocation)
		if err != nil {
			return acceptance.Acceptance{}, fmt.Errorf("parsing allocation: %w", err)
		}
		acc.Allocation = &link
	}
	return acc, nil
}
