package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/pgx/v5"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

// NewStagingIndex returns the blobstore.StagingIndex kept in pdp_staged_blobs:
// which staged upload holds the bytes of a blob received without its digest,
// until the settle task moves them to the key of their digest.
func NewStagingIndex(db *harmonydb.DB) blobstore.StagingIndex {
	return stagingIndex{db: db}
}

type stagingIndex struct {
	db *harmonydb.DB
}

func (u stagingIndex) GetID(ctx context.Context, digest multihash.Multihash) (string, bool, error) {
	var id string
	err := harmonytx.Run(ctx, u.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		return tx.QueryRow(`SELECT upload_id FROM pdp_staged_blobs WHERE digest = $1`, []byte(digest)).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading pdp_staged_blobs: %w", err)
	}
	return id, true, nil
}

func (u stagingIndex) Delete(ctx context.Context, digest multihash.Multihash) error {
	if err := harmonytx.Run(ctx, u.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`DELETE FROM pdp_staged_blobs WHERE digest = $1`, []byte(digest))
		return err
	}); err != nil {
		return fmt.Errorf("deleting from pdp_staged_blobs: %w", err)
	}
	return nil
}
