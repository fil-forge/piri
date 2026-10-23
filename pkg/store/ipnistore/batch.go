package ipnistore

import (
	"context"
	"fmt"

	"github.com/fil-forge/go-ipni-tools/pkg/metadata"
	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/ipfs/go-cid"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/dagsync/ipnisync/head"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
)

var _ store.BatchStore = (*Store)(nil)

// putAdvertsConcurrency bounds how many adverts a batch writes at once.
const putAdvertsConcurrency = 16

// LoadContexts reads the records of the context IDs with one statement.
func (s *Store) LoadContexts(ctx context.Context, p peer.ID, contextIDs [][]byte) (map[string]store.ContextRecord, error) {
	if err := s.checkProvider(p); err != nil {
		return nil, err
	}
	var rows []struct {
		ContextID []byte `db:"context_id"`
		ChunkLink []byte `db:"chunk_link"`
		Metadata  []byte `db:"metadata"`
	}
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		rows = rows[:0]
		return tx.Select(&rows, `
			SELECT context_id, chunk_link, metadata FROM ipni_contexts
			WHERE context_id = ANY($1)`, contextIDs)
	})
	if err != nil {
		return nil, fmt.Errorf("reading IPNI contexts for %d context IDs: %w", len(contextIDs), err)
	}
	records := make(map[string]store.ContextRecord, len(rows))
	for _, r := range rows {
		var record store.ContextRecord
		if r.ChunkLink != nil {
			_, c, err := cid.CidFromBytes(r.ChunkLink)
			if err != nil {
				return nil, fmt.Errorf("decoding IPNI entries link for context %x: %w", r.ContextID, err)
			}
			record.ChunkLink = cidlink.Link{Cid: c}
		}
		if r.Metadata != nil {
			md := metadata.MetadataContext.New()
			if err := md.UnmarshalBinary(r.Metadata); err != nil {
				return nil, fmt.Errorf("decoding IPNI metadata for context %x: %w", r.ContextID, err)
			}
			record.Metadata = &md
		}
		records[string(r.ContextID)] = record
	}
	return records, nil
}

// PutAdverts writes the adverts to the object store in parallel. On a
// failure it returns the error once the writes in flight have finished;
// adverts already written stay, unreferenced until a head links to them.
func (s *Store) PutAdverts(ctx context.Context, advs []schema.Advertisement) (err error) {
	ctx, span := tracer.Start(ctx, "ipni.put_adverts", trace.WithAttributes(attribute.Int("ipni.advert_count", len(advs))))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(putAdvertsConcurrency)
	for _, adv := range advs {
		group.Go(func() error {
			_, err := store.PutAdvert(ctx, s.blocks, adv)
			return err
		})
	}
	return group.Wait()
}

// CommitBatch writes the records with one upsert and one delete, then moves
// the head by compare-and-swap, all in one transaction: the one the context
// carries, or its own. A failure leaves the records to the rollback.
func (s *Store) CommitBatch(ctx context.Context, p peer.ID, changes []store.ContextChange, oldHead, newHead *head.SignedHead) error {
	if err := s.checkProvider(p); err != nil {
		return err
	}
	var putIDs, chunkLinks, metas, deleteIDs [][]byte
	for _, c := range changes {
		if c.New.ChunkLink == nil && c.New.Metadata == nil {
			deleteIDs = append(deleteIDs, c.ContextID)
			continue
		}
		var chunkLink, meta []byte
		if c.New.ChunkLink != nil {
			chunkLink = []byte(c.New.ChunkLink.Binary())
		}
		if c.New.Metadata != nil {
			var err error
			if meta, err = c.New.Metadata.MarshalBinary(); err != nil {
				return fmt.Errorf("encoding IPNI metadata for context %x: %w", c.ContextID, err)
			}
		}
		putIDs = append(putIDs, c.ContextID)
		chunkLinks = append(chunkLinks, chunkLink)
		metas = append(metas, meta)
	}
	return harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		if len(putIDs) > 0 {
			if _, err := tx.Exec(`
				INSERT INTO ipni_contexts (context_id, chunk_link, metadata)
				SELECT * FROM unnest($1::bytea[], $2::bytea[], $3::bytea[])
				ON CONFLICT (context_id) DO UPDATE
				SET chunk_link = EXCLUDED.chunk_link, metadata = EXCLUDED.metadata`,
				putIDs, chunkLinks, metas); err != nil {
				return fmt.Errorf("writing %d IPNI contexts: %w", len(putIDs), err)
			}
		}
		if len(deleteIDs) > 0 {
			if _, err := tx.Exec(`DELETE FROM ipni_contexts WHERE context_id = ANY($1)`, deleteIDs); err != nil {
				return fmt.Errorf("deleting %d IPNI contexts: %w", len(deleteIDs), err)
			}
		}
		_, err := store.ReplaceHead(ctx, s.blocks, oldHead, newHead)
		return err
	})
}

// checkProvider fails for any provider but this node: the table does not
// store one, so another provider's mapping would overwrite the node's own.
func (s *Store) checkProvider(p peer.ID) error {
	if p != s.provider {
		return fmt.Errorf("IPNI store holds provider %s only, not %s", s.provider, p)
	}
	return nil
}
