package ipnistore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/yugabyte/pgx/v5"
	"go.opentelemetry.io/otel"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	"github.com/fil-forge/piri/pkg/store/objectstore"
)

var tracer = otel.Tracer("github.com/fil-forge/piri/pkg/store/ipnistore")

// headKey is the key go-ipni-tools reads and replaces the signed head under.
// Every other key is the CID of a block.
const headKey = "head"

// blockStore is go-ipni-tools' key-value store: the head is the ipni_head row
// and every other key is an object.
type blockStore struct {
	db      *harmonydb.DB
	objects objectstore.Store
}

var _ store.Store = (*blockStore)(nil)

func (s *blockStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if key == headKey {
		return s.getHead(ctx)
	}
	obj, err := s.objects.Get(ctx, key)
	if errors.Is(err, objectstore.ErrNotExist) {
		return nil, store.NewErrNotFound(err)
	}
	if err != nil {
		return nil, fmt.Errorf("getting IPNI block %s: %w", key, err)
	}
	return obj.Body(), nil
}

func (s *blockStore) Put(ctx context.Context, key string, size uint64, data io.Reader) error {
	if key == headKey {
		return errors.New("the IPNI head is written with Replace, not Put")
	}
	if err := s.objects.Put(ctx, key, size, data); err != nil {
		return fmt.Errorf("putting IPNI block %s: %w", key, err)
	}
	return nil
}

// Replace moves the head from old to new, and fails with
// store.ErrPreconditionFailed when the head is no longer old. A nil old
// means no head has been written yet.
func (s *blockStore) Replace(ctx context.Context, key string, old io.Reader, size uint64, new io.Reader) error {
	if key != headKey {
		return fmt.Errorf("only the IPNI head can be replaced, not %q", key)
	}
	newHead, err := io.ReadAll(new)
	if err != nil {
		return fmt.Errorf("reading new IPNI head: %w", err)
	}
	var oldHead []byte
	if old != nil {
		if oldHead, err = io.ReadAll(old); err != nil {
			return fmt.Errorf("reading old IPNI head: %w", err)
		}
	}
	var replaced int
	err = harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		if old == nil {
			replaced, err = tx.Exec(`INSERT INTO ipni_head (head) VALUES ($1) ON CONFLICT DO NOTHING`, newHead)
			return err
		}
		replaced, err = tx.Exec(`UPDATE ipni_head SET head = $1 WHERE head = $2`, newHead, oldHead)
		return err
	})
	if err != nil {
		return fmt.Errorf("replacing IPNI head: %w", err)
	}
	if replaced == 0 {
		return store.ErrPreconditionFailed
	}
	return nil
}

func (s *blockStore) getHead(ctx context.Context) (io.ReadCloser, error) {
	var head []byte
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		return tx.QueryRow(`SELECT head FROM ipni_head`).Scan(&head)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, store.NewErrNotFound(err)
	}
	if err != nil {
		return nil, fmt.Errorf("getting IPNI head: %w", err)
	}
	return io.NopCloser(bytes.NewReader(head)), nil
}
