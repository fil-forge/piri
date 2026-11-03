package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/pgx/v5"
	"go.opentelemetry.io/otel/codes"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
)

// Entry is the pipeline's ingress: it implements commp.Calculator so
// /blob/accept (and replica transfer) can hand a blob to the pipeline.
// Enqueue records the blob in pdp_blob_pipeline, in the caller's transaction
// when the context carries one (see harmonytx), so the entry commits with the
// acceptance that motivated it. The task spawn waits for the commit and is
// best-effort (AddTask swallows errors); rows that miss their spawn are
// scavenged by the task's IAmBored. A blob that is still staged starts at the
// PDPSettle task, any other at PDPCommP.
type Entry struct {
	db     *harmonydb.DB
	commp  *CommPTask
	settle *SettleTask
}

func NewEntry(db *harmonydb.DB, commp *CommPTask, settle *SettleTask) *Entry {
	return &Entry{db: db, commp: commp, settle: settle}
}

var _ commp.Calculator = (*Entry)(nil)

func (e *Entry) Enqueue(ctx context.Context, blob multihash.Multihash) (err error) {
	ctx, span := tracer.Start(ctx, "commp.enqueue")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	log.Infow("enqueuing blob for aggregation", "blob", blob.String())
	err = harmonytx.Run(ctx, e.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		var staged bool
		// A blob whose piece is already live on-chain (or staged to be) is a
		// re-accept of content the pipeline has already carried through —
		// no new entry. Pieces scheduled for removal (rm_message_hash set)
		// do NOT count as live: their root is leaving the proof set, so
		// revived content must ride the pipeline again under a new root.
		var live bool
		if err := tx.QueryRow(`
			SELECT EXISTS (
				SELECT 1
				FROM pdp_piece_mh_to_commp m
				WHERE m.mhash = $1
				  AND (
					EXISTS (
						SELECT 1 FROM pdp_data_set_pieces p
						WHERE p.sub_piece = m.commp_v1
						  AND p.removed = FALSE
						  AND p.rm_message_hash IS NULL
					)
					OR EXISTS (
						SELECT 1 FROM pdp_data_set_piece_adds a
						WHERE a.sub_piece = m.commp_v1
						  AND a.pieces_added = FALSE
						  AND (a.add_message_ok IS NULL OR a.add_message_ok = TRUE)
					)
				  )
			)`, []byte(blob)).Scan(&live); err != nil {
			return fmt.Errorf("checking live pieces: %w", err)
		}
		if live {
			log.Infow("blob already staged or proven, skipping pipeline entry", "blob", blob.String())
			return nil
		}
		// The blob's upload has completed by now, so a blob received without
		// its digest is staged here until its settle task moves it.
		err := tx.QueryRow(`
			INSERT INTO pdp_blob_pipeline (digest, staged)
			VALUES ($1, EXISTS (SELECT 1 FROM pdp_staged_blobs WHERE digest = $1))
			ON CONFLICT (digest) DO NOTHING
			RETURNING staged
		`, []byte(blob)).Scan(&staged)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inserting pipeline entry: %w", err)
		}
		// The task reads the row, so it is spawned once the row commits.
		return harmonytx.AfterCommit(ctx, func(ctx context.Context) error {
			if staged {
				e.settle.spawn(ctx, blob)
			} else {
				e.commp.spawn(ctx, blob)
			}
			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("enqueuing blob %s for aggregation: %w", blob.String(), err)
	}
	return nil
}
