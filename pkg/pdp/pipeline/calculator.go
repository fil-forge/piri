package pipeline

import (
	"context"
	"fmt"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
)

// Entry is the pipeline's ingress: it implements commp.Calculator so
// /blob/accept (and replica transfer) can hand a blob to the pipeline.
// Enqueue durably records the blob in pdp_blob_pipeline before returning —
// the caller's acceptance-compensation logic relies on Enqueue reporting
// failure — while the PDPCommP task spawn is best-effort (AddTask swallows
// errors); rows that miss their spawn are scavenged by the task's IAmBored.
type Entry struct {
	db    *harmonydb.DB
	commp *CommPTask
}

func NewEntry(db *harmonydb.DB, commp *CommPTask) *Entry {
	return &Entry{db: db, commp: commp}
}

var _ commp.Calculator = (*Entry)(nil)

func (e *Entry) Enqueue(ctx context.Context, blob multihash.Multihash) error {
	log.Infow("enqueuing blob for aggregation", "blob", blob.String())
	var spawn bool
	_, err := e.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		spawn = false
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
			return false, fmt.Errorf("checking live pieces: %w", err)
		}
		if live {
			log.Infow("blob already staged or proven, skipping pipeline entry", "blob", blob.String())
			return false, nil
		}
		n, err := tx.Exec(`
			INSERT INTO pdp_blob_pipeline (digest) VALUES ($1)
			ON CONFLICT (digest) DO NOTHING
		`, []byte(blob))
		if err != nil {
			return false, fmt.Errorf("inserting pipeline entry: %w", err)
		}
		spawn = n > 0
		return spawn, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return fmt.Errorf("enqueuing blob %s for aggregation: %w", blob.String(), err)
	}
	if spawn {
		e.commp.spawn(ctx, blob)
	}
	return nil
}
