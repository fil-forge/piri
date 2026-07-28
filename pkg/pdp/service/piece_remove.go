package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/pgx/v5"

	"github.com/fil-forge/piri/pkg/store"
)

// RemovePiece records a request to release a blob's bytes. It never deletes
// anything inline: point-in-time state (acceptances, aggregation progress,
// on-chain piece rows) races against the concurrently-advancing pipeline,
// so classification is deferred to the removal machinery, which re-derives
// everything per pass and only acts when it can prove nothing references
// the bytes. Idempotent.
func (p *PDPService) RemovePiece(ctx context.Context, blob multihash.Multihash) (retErr error) {
	log.Infow("queueing piece removal", "blob", blob.String())
	defer func() {
		if retErr != nil {
			log.Errorw("failed to queue piece removal", "blob", blob.String(), "err", retErr)
		}
	}()

	if _, err := p.db.Exec(ctx, `
		INSERT INTO pdp_pending_piece_removals (digest) VALUES ($1)
		ON CONFLICT (digest) DO NOTHING
	`, []byte(blob)); err != nil {
		return fmt.Errorf("recording pending piece removal: %w", err)
	}
	return nil
}

// ProcessPendingRemovals advances queued blob removals. For each pending
// blob, each pass:
//
//  1. Re-checks claims: any acceptance or allocation (in any space) cancels
//     the removal — a racing accept or a re-upload revived the blob.
//  2. Cancels the blob's pipeline row if it hasn't been folded into an
//     aggregate (transactional delete, serialized against the fold's row
//     locks; an in-flight commp task no-ops on the missing row).
//  3. Resolves the commp mapping. A blob with no mapping and no pipeline
//     row was never aggregated and never will be — its bytes are released.
//     Staged-but-unconfirmed piece adds park the removal until the chain
//     watcher settles them.
//  4. A blob whose piece is live on-chain rides the root lifecycle: roots
//     whose every sub-piece is pending removal are retired via RemoveRoot
//     (schedulePieceDeletions stamps rm_message_hash); the NextProvingPeriod
//     task then either confirms the deletion (removed=TRUE) or clears the
//     stamp on a failed transaction, which reschedules it here. Roots with
//     any live sub-piece keep being proven — and the pending blob's bytes
//     retained — until the whole root is retirable.
//  5. Once nothing on-chain or in-pipeline references the piece, the blob
//     is finalized: bookkeeping rows (data-set pieces, piece refs, parked
//     pieces, commp mapping) are deleted and the bytes are released. Claims
//     are re-checked immediately before deletion.
//
// Each sweep is incremental and safe to run repeatedly.
func (p *PDPService) ProcessPendingRemovals(ctx context.Context) error {
	return p.processPendingRemovals(ctx, p.RemoveRoot)
}

// processPendingRemovals implements ProcessPendingRemovals with the on-chain
// root removal injected so tests can exercise the sweep without contracts.
func (p *PDPService) processPendingRemovals(ctx context.Context, removeRoot func(context.Context, uint64, uint64) (common.Hash, error)) error {
	var pending []struct {
		Blob []byte `db:"digest"`
	}
	if err := p.db.Select(ctx, &pending, `SELECT digest FROM pdp_pending_piece_removals`); err != nil {
		return fmt.Errorf("listing pending removals: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	type rootKey struct {
		dataSet int64
		pieceID int64
	}
	pendingSubs := make(map[rootKey]map[string]struct{})

	var errs error
	for _, r := range pending {
		blob := multihash.Multihash(r.Blob)

		// Claims re-check: a racing accept or re-allocation of the same
		// content revives the blob; the removal request is obsolete.
		revived, err := p.blobHasClaims(ctx, blob)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		if revived {
			if err := p.cancelPendingRemoval(ctx, blob); err != nil {
				errs = errors.Join(errs, err)
			}
			continue
		}

		// Cancel the pipeline row while it is still pre-aggregation; past
		// that point the piece must ride the root lifecycle.
		pipelineActive, err := p.cancelPipelineEntry(ctx, blob)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}

		var commp, commpV1 string
		err = p.db.QueryRow(ctx, `
			SELECT commp, commp_v1 FROM pdp_piece_mh_to_commp WHERE mhash = $1
		`, r.Blob).Scan(&commp, &commpV1)
		if errors.Is(err, pgx.ErrNoRows) {
			if pipelineActive {
				// Mid-pipeline without a mapping: the commp stage hasn't
				// finished; wait for the next pass rather than guess.
				continue
			}
			// No mapping and no pipeline row: the bytes were never
			// aggregated and, with the row cancelled, never will be.
			if err := p.finalizeRemoval(ctx, blob, "", ""); err != nil {
				errs = errors.Join(errs, err)
			}
			continue
		}
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("resolving commp for %s: %w", blob.String(), err))
			continue
		}

		// Staged-but-unconfirmed adds: wait for the chain watcher to settle
		// them into pdp_data_set_pieces (or fail the add) before classifying.
		var pendingAdds int
		if err := p.db.QueryRow(ctx, `
			SELECT count(*) FROM pdp_data_set_piece_adds
			WHERE sub_piece = $1 AND pieces_added = FALSE
			  AND (add_message_ok IS NULL OR add_message_ok = TRUE)
		`, commpV1).Scan(&pendingAdds); err != nil {
			errs = errors.Join(errs, fmt.Errorf("counting pending adds for %s: %w", blob.String(), err))
			continue
		}
		if pendingAdds > 0 {
			continue
		}

		var pieceRows []struct {
			DataSet int64   `db:"data_set"`
			PieceID int64   `db:"piece_id"`
			RmHash  *string `db:"rm_message_hash"`
			Removed bool    `db:"removed"`
		}
		if err := p.db.Select(ctx, &pieceRows, `
			SELECT data_set, piece_id, rm_message_hash, removed
			FROM pdp_data_set_pieces WHERE sub_piece = $1
		`, commpV1); err != nil {
			errs = errors.Join(errs, fmt.Errorf("finding piece rows for %s: %w", blob.String(), err))
			continue
		}

		var live []rootKey
		inFlight := false
		for _, pr := range pieceRows {
			if pr.Removed {
				continue
			}
			if pr.RmHash != nil {
				// Deletion tx in flight: NextProvingPeriod confirms it or
				// clears the stamp on failure, rescheduling it here.
				inFlight = true
				continue
			}
			live = append(live, rootKey{pr.DataSet, pr.PieceID})
		}
		if inFlight {
			continue
		}
		if len(live) == 0 {
			// Nothing proven (never staged, or every deletion confirmed).
			// Piece refs normally mean aggregation is in flight — but only
			// if the pipeline row still exists; refs orphaned by a cancelled
			// row are dead bookkeeping that finalization cleans up.
			var refCount int
			if err := p.db.QueryRow(ctx, `
				SELECT count(*) FROM pdp_piecerefs WHERE piece_cid = $1
			`, commp).Scan(&refCount); err != nil {
				errs = errors.Join(errs, fmt.Errorf("counting piece refs for %s: %w", blob.String(), err))
				continue
			}
			if refCount == 0 || !pipelineActive {
				if err := p.finalizeRemoval(ctx, blob, commp, commpV1); err != nil {
					errs = errors.Join(errs, err)
				}
			}
			continue
		}
		for _, k := range live {
			if pendingSubs[k] == nil {
				pendingSubs[k] = make(map[string]struct{})
			}
			pendingSubs[k][commpV1] = struct{}{}
		}
	}

	// Retire roots whose every live sub-piece belongs to a pending removal.
	for k, subs := range pendingSubs {
		var total int
		if err := p.db.QueryRow(ctx, `
			SELECT count(DISTINCT sub_piece) FROM pdp_data_set_pieces
			WHERE data_set = $1 AND piece_id = $2 AND removed = FALSE
		`, k.dataSet, k.pieceID).Scan(&total); err != nil {
			errs = errors.Join(errs, fmt.Errorf("counting sub-pieces of piece %d: %w", k.pieceID, err))
			continue
		}
		if len(subs) < total {
			continue // root still has live sub-pieces; keep proving it
		}
		txHash, err := removeRoot(ctx, uint64(k.dataSet), uint64(k.pieceID))
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("removing piece %d from data set %d: %w", k.pieceID, k.dataSet, err))
			continue
		}
		log.Infow("scheduled on-chain piece deletion",
			"dataSet", k.dataSet, "pieceID", k.pieceID, "tx", txHash)
	}
	return errs
}

// blobHasClaims reports whether any space holds an acceptance or allocation
// for the digest — the signal that a removal request has been overtaken by a
// racing accept or a fresh upload of the same content.
func (p *PDPService) blobHasClaims(ctx context.Context, blob multihash.Multihash) (bool, error) {
	accepted, err := p.acceptanceStore.Exists(ctx, blob)
	if err != nil {
		return false, fmt.Errorf("checking acceptances for %s: %w", blob.String(), err)
	}
	if accepted {
		return true, nil
	}
	allocated, err := p.allocationStore.Exists(ctx, blob)
	if err != nil {
		return false, fmt.Errorf("checking allocations for %s: %w", blob.String(), err)
	}
	return allocated, nil
}

// cancelPendingRemoval drops the removal request, keeping the bytes.
func (p *PDPService) cancelPendingRemoval(ctx context.Context, blob multihash.Multihash) error {
	if _, err := p.db.Exec(ctx, `
		DELETE FROM pdp_pending_piece_removals WHERE digest = $1
	`, []byte(blob)); err != nil {
		return fmt.Errorf("cancelling pending removal of %s: %w", blob.String(), err)
	}
	log.Infow("cancelled piece removal; blob has live claims", "blob", blob.String())
	return nil
}

// cancelPipelineEntry deletes the blob's pdp_blob_pipeline row if it has not
// been folded into an aggregate, and reports whether a row remains
// afterwards. The aggregation fold locks its candidate rows (FOR UPDATE), so
// this delete serializes against it: a row is either cancelled or folded —
// never both. An in-flight commp task whose row disappears completes as a
// noop.
func (p *PDPService) cancelPipelineEntry(ctx context.Context, blob multihash.Multihash) (active bool, err error) {
	if _, err := p.db.Exec(ctx, `
		DELETE FROM pdp_blob_pipeline WHERE digest = $1 AND aggregate_root IS NULL
	`, []byte(blob)); err != nil {
		return false, fmt.Errorf("cancelling pipeline row for %s: %w", blob.String(), err)
	}
	var remaining int
	if err := p.db.QueryRow(ctx, `
		SELECT count(*) FROM pdp_blob_pipeline WHERE digest = $1
	`, []byte(blob)).Scan(&remaining); err != nil {
		return false, fmt.Errorf("checking pipeline row for %s: %w", blob.String(), err)
	}
	return remaining > 0, nil
}

// finalizeRemoval deletes the blob's bytes and every bookkeeping row tied to
// its piece: confirmed-removed data-set piece rows, the pdp_piecerefs /
// parked_piece_refs / parked_pieces chain, the mhash→commp mapping, and the
// pending-removal row itself. Claims are re-checked at the last instant: if
// the blob was revived after this sweep pass classified it, the removal is
// cancelled instead. commp/commpV1 are empty when the blob never had a
// mapping.
func (p *PDPService) finalizeRemoval(ctx context.Context, blob multihash.Multihash, commp, commpV1 string) error {
	revived, err := p.blobHasClaims(ctx, blob)
	if err != nil {
		return err
	}
	if revived {
		return p.cancelPendingRemoval(ctx, blob)
	}

	if err := p.blobstore.Delete(ctx, blob); err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("deleting blob bytes: %w", err)
	}
	if _, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		if commpV1 != "" {
			if _, err := tx.Exec(`
				DELETE FROM pdp_data_set_pieces WHERE sub_piece = $1 AND removed = TRUE
			`, commpV1); err != nil {
				return false, fmt.Errorf("deleting removed piece rows: %w", err)
			}
		}
		if commp != "" {
			var refs []struct {
				RefID   int64 `db:"piece_ref"`
				PieceID int64 `db:"piece_id"`
			}
			if err := tx.Select(&refs, `
				SELECT pr.piece_ref, ppr.piece_id
				FROM pdp_piecerefs pr
				JOIN parked_piece_refs ppr ON ppr.ref_id = pr.piece_ref
				WHERE pr.piece_cid = $1
			`, commp); err != nil {
				return false, fmt.Errorf("finding piece refs: %w", err)
			}
			if _, err := tx.Exec(`
				DELETE FROM pdp_piecerefs WHERE piece_cid = $1
			`, commp); err != nil {
				return false, fmt.Errorf("deleting pdp piece refs: %w", err)
			}
			for _, ref := range refs {
				if _, err := tx.Exec(`
					DELETE FROM parked_piece_refs WHERE ref_id = $1
				`, ref.RefID); err != nil {
					return false, fmt.Errorf("deleting parked piece ref: %w", err)
				}
				// Release the parked piece only once no other refs share it.
				if _, err := tx.Exec(`
					DELETE FROM parked_pieces
					WHERE id = $1 AND NOT EXISTS (
						SELECT 1 FROM parked_piece_refs WHERE piece_id = $1
					)
				`, ref.PieceID); err != nil {
					return false, fmt.Errorf("deleting parked piece: %w", err)
				}
			}
		}
		if _, err := tx.Exec(`
			DELETE FROM pdp_piece_mh_to_commp WHERE mhash = $1
		`, []byte(blob)); err != nil {
			return false, fmt.Errorf("deleting mhash to commp mapping: %w", err)
		}
		if _, err := tx.Exec(`
			DELETE FROM pdp_pending_piece_removals WHERE digest = $1
		`, []byte(blob)); err != nil {
			return false, fmt.Errorf("deleting pending removal row: %w", err)
		}
		return true, nil
	}, harmonydb.OptionRetry()); err != nil {
		return fmt.Errorf("finalizing removal of blob %s: %w", blob.String(), err)
	}
	log.Infow("finalized piece removal", "blob", blob.String())
	return nil
}
