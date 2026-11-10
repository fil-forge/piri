package ipnistore

import (
	"context"
	"errors"
	"fmt"

	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/yugabyte/pgx/v5"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
)

// column names one of the two maps ipni_contexts holds.
type column int

const (
	chunkLinkColumn column = iota
	metadataColumn
)

func (c column) String() string {
	if c == chunkLinkColumn {
		return "chunk_link"
	}
	return "metadata"
}

// contextTable is one go-ipni-tools map from context ID to bytes, kept in its
// column of ipni_contexts. A row holds both maps of a context ID and goes
// once both are cleared.
type contextTable struct {
	db       *harmonydb.DB
	provider peer.ID
	column   column
}

var _ store.ProviderContextTable = (*contextTable)(nil)

func (t *contextTable) Get(ctx context.Context, p peer.ID, contextID []byte) ([]byte, error) {
	if err := t.checkProvider(p); err != nil {
		return nil, err
	}
	var data []byte
	err := harmonytx.Run(ctx, t.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		if t.column == chunkLinkColumn {
			return tx.QueryRow(`SELECT chunk_link FROM ipni_contexts WHERE context_id = $1 AND chunk_link IS NOT NULL`, contextID).Scan(&data)
		}
		return tx.QueryRow(`SELECT metadata FROM ipni_contexts WHERE context_id = $1 AND metadata IS NOT NULL`, contextID).Scan(&data)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, store.NewErrNotFound(err)
	}
	if err != nil {
		return nil, fmt.Errorf("getting IPNI %s for context %x: %w", t.column, contextID, err)
	}
	return data, nil
}

func (t *contextTable) Put(ctx context.Context, p peer.ID, contextID []byte, data []byte) error {
	if err := t.checkProvider(p); err != nil {
		return err
	}
	if data == nil {
		// A NULL column reads as no mapping, so it cannot hold one.
		return fmt.Errorf("putting IPNI %s for context %x: no data", t.column, contextID)
	}
	err := harmonytx.Run(ctx, t.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		var err error
		if t.column == chunkLinkColumn {
			_, err = tx.Exec(`
				INSERT INTO ipni_contexts (context_id, chunk_link) VALUES ($1, $2)
				ON CONFLICT (context_id) DO UPDATE SET chunk_link = EXCLUDED.chunk_link`, contextID, data)
		} else {
			_, err = tx.Exec(`
				INSERT INTO ipni_contexts (context_id, metadata) VALUES ($1, $2)
				ON CONFLICT (context_id) DO UPDATE SET metadata = EXCLUDED.metadata`, contextID, data)
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("putting IPNI %s for context %x: %w", t.column, contextID, err)
	}
	return nil
}

func (t *contextTable) Delete(ctx context.Context, p peer.ID, contextID []byte) error {
	if err := t.checkProvider(p); err != nil {
		return err
	}
	err := harmonytx.Run(ctx, t.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		var err error
		if t.column == chunkLinkColumn {
			_, err = tx.Exec(`UPDATE ipni_contexts SET chunk_link = NULL WHERE context_id = $1`, contextID)
		} else {
			_, err = tx.Exec(`UPDATE ipni_contexts SET metadata = NULL WHERE context_id = $1`, contextID)
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM ipni_contexts WHERE context_id = $1 AND chunk_link IS NULL AND metadata IS NULL`, contextID)
		return err
	})
	if err != nil {
		return fmt.Errorf("deleting IPNI %s for context %x: %w", t.column, contextID, err)
	}
	return nil
}

// checkProvider fails for any provider but this node: the table does not
// store one, so another provider's mapping would overwrite the node's own.
func (t *contextTable) checkProvider(p peer.ID) error {
	if p != t.provider {
		return fmt.Errorf("IPNI context table holds provider %s only, not %s", t.provider, p)
	}
	return nil
}
