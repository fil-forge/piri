package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/pgx/v5"

	"github.com/fil-forge/piri/pkg/store/blobstore"
)

// NewUploadIndex returns the blobstore.UploadIndex kept in pdp_blob_uploads:
// which upload holds the bytes of a blob received without its digest, until
// the commP task settles them at the key of their digest.
func NewUploadIndex(db *harmonydb.DB) blobstore.UploadIndex {
	return uploadIndex{db: db}
}

type uploadIndex struct {
	db *harmonydb.DB
}

func (u uploadIndex) Upload(ctx context.Context, digest multihash.Multihash) (string, bool, error) {
	var id string
	err := u.db.QueryRow(ctx, `SELECT upload_id FROM pdp_blob_uploads WHERE digest = $1`, []byte(digest)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading pdp_blob_uploads: %w", err)
	}
	return id, true, nil
}

func (u uploadIndex) Forget(ctx context.Context, digest multihash.Multihash) error {
	if _, err := u.db.Exec(ctx, `DELETE FROM pdp_blob_uploads WHERE digest = $1`, []byte(digest)); err != nil {
		return fmt.Errorf("deleting from pdp_blob_uploads: %w", err)
	}
	return nil
}
