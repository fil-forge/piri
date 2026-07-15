package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	libpiece "github.com/fil-forge/libforge/piece"
	"github.com/multiformats/go-multihash"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/fil-forge/piri/pkg/pdp/service/models"
	"github.com/fil-forge/piri/pkg/store"
)

// RemovePiece releases the bytes of a blob that no longer has any space
// claiming it. If the blob was never aggregated (no commp mapping) its bytes
// are deleted immediately. Otherwise the blob's piece is a subroot of a live
// aggregate root that piri must keep proving until the whole root is retired
// on-chain, so the blob is queued in pdp_pending_removals and
// ProcessPendingRemovals completes the removal asynchronously. Idempotent.
func (p *PDPService) RemovePiece(ctx context.Context, blob multihash.Multihash) (retErr error) {
	log.Infow("removing piece", "blob", blob)
	defer func() {
		if retErr != nil {
			log.Errorw("failed to remove piece", "blob", blob, "err", retErr)
		}
	}()

	commp, found, err := p.pieceResolver.ResolveToPiece(ctx, blob)
	if err != nil {
		return fmt.Errorf("resolving blob to piece: %w", err)
	}
	if !found {
		// Never aggregated: nothing on-chain references these bytes.
		if err := p.blobstore.Delete(ctx, blob); err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("deleting blob bytes: %w", err)
		}
		return nil
	}

	removal := models.PDPPendingRemoval{
		Blob:  blob,
		Commp: libpiece.MultihashToCommpCID(commp).String(),
		State: models.PendingRemovalStatePending,
	}
	if err := p.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&removal).Error; err != nil {
		return fmt.Errorf("recording pending removal: %w", err)
	}
	return nil
}

// ProcessPendingRemovals advances queued blob removals. It runs in three
// phases:
//
//  1. Pending blobs whose piece is not (or no longer) part of any proof set
//     are finalized immediately — unless the piece still has a pdp_piecerefs
//     row, which means aggregation is in flight and the sweep must wait.
//  2. Roots whose every subroot is pending removal are retired on-chain via
//     RemoveRoot (schedulePieceDeletions); their rows move to "scheduled".
//  3. Scheduled blobs finalize once the root's pdp_proofset_roots rows are
//     gone (the prove task's cleanupDeletedRoots reaps them after the chain
//     executes the removal). A failed transaction resets rows to "pending"
//     so the next sweep reschedules.
//
// Each sweep is incremental and safe to run repeatedly.
func (p *PDPService) ProcessPendingRemovals(ctx context.Context) error {
	return p.processPendingRemovals(ctx, p.RemoveRoot)
}

// processPendingRemovals implements ProcessPendingRemovals with the on-chain
// root removal injected so tests can exercise the sweep without contracts.
func (p *PDPService) processPendingRemovals(ctx context.Context, removeRoot func(context.Context, uint64, uint64) (common.Hash, error)) error {
	var removals []models.PDPPendingRemoval
	if err := p.db.WithContext(ctx).Find(&removals).Error; err != nil {
		return fmt.Errorf("listing pending removals: %w", err)
	}
	if len(removals) == 0 {
		return nil
	}

	var errs error
	pendingByRoot := make(map[rootKey][]models.PDPPendingRemoval)
	for _, r := range removals {
		switch r.State {
		case models.PendingRemovalStatePending:
			var subroots []models.PDPProofsetRoot
			if err := p.db.WithContext(ctx).
				Where("subroot = ?", r.Commp).
				Find(&subroots).Error; err != nil {
				errs = errors.Join(errs, fmt.Errorf("finding subroot rows for %s: %w", r.Commp, err))
				continue
			}
			if len(subroots) == 0 {
				// Not part of any proof set. If a piece ref exists the piece
				// is parked and awaiting aggregation — removing now would
				// corrupt the in-flight root, so wait for it to land.
				var refCount int64
				if err := p.db.WithContext(ctx).
					Model(&models.PDPPieceRef{}).
					Where("piece_cid = ?", r.Commp).
					Count(&refCount).Error; err != nil {
					errs = errors.Join(errs, fmt.Errorf("counting piece refs for %s: %w", r.Commp, err))
					continue
				}
				if refCount == 0 {
					if err := p.finalizeRemoval(ctx, r); err != nil {
						errs = errors.Join(errs, err)
					}
				}
				continue
			}
			for _, sr := range subroots {
				k := rootKey{sr.ProofsetID, sr.RootID}
				pendingByRoot[k] = append(pendingByRoot[k], r)
			}
		case models.PendingRemovalStateScheduled:
			if err := p.advanceScheduledRemoval(ctx, r); err != nil {
				errs = errors.Join(errs, err)
			}
		}
	}

	// Retire roots whose subroots are all pending removal.
	for k, pending := range pendingByRoot {
		var total int64
		if err := p.db.WithContext(ctx).
			Model(&models.PDPProofsetRoot{}).
			Where("proofset_id = ? AND root_id = ?", k.proofsetID, k.rootID).
			Count(&total).Error; err != nil {
			errs = errors.Join(errs, fmt.Errorf("counting subroots of root %d: %w", k.rootID, err))
			continue
		}
		if int64(len(pending)) < total {
			continue // root still has live subroots; keep proving it
		}
		txHash, err := removeRoot(ctx, uint64(k.proofsetID), uint64(k.rootID))
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("removing root %d from proof set %d: %w", k.rootID, k.proofsetID, err))
			continue
		}
		hashStr := txHash.String()
		blobs := make([][]byte, 0, len(pending))
		for _, r := range pending {
			blobs = append(blobs, r.Blob)
		}
		if err := p.db.WithContext(ctx).
			Model(&models.PDPPendingRemoval{}).
			Where("blob IN ?", blobs).
			Updates(map[string]any{
				"state":               models.PendingRemovalStateScheduled,
				"proofset_id":         k.proofsetID,
				"root_id":             k.rootID,
				"remove_message_hash": hashStr,
			}).Error; err != nil {
			errs = errors.Join(errs, fmt.Errorf("marking removals scheduled for root %d: %w", k.rootID, err))
		}
	}
	return errs
}

type rootKey struct {
	proofsetID int64
	rootID     int64
}

// advanceScheduledRemoval finalizes a scheduled removal once its root has
// been reaped from pdp_proofset_roots, or resets it to pending if the
// removal transaction failed.
func (p *PDPService) advanceScheduledRemoval(ctx context.Context, r models.PDPPendingRemoval) error {
	if r.ProofsetID == nil || r.RootID == nil {
		// Invariant violation; reset so the sweep re-derives the root.
		return p.db.WithContext(ctx).
			Model(&models.PDPPendingRemoval{}).
			Where("blob = ?", r.Blob).
			Update("state", models.PendingRemovalStatePending).Error
	}

	var rootCount int64
	if err := p.db.WithContext(ctx).
		Model(&models.PDPProofsetRoot{}).
		Where("proofset_id = ? AND root_id = ?", *r.ProofsetID, *r.RootID).
		Count(&rootCount).Error; err != nil {
		return fmt.Errorf("counting root rows for scheduled removal: %w", err)
	}
	if rootCount == 0 {
		// cleanupDeletedRoots has reaped the root — the chain executed the
		// removal and the bytes are no longer proven.
		return p.finalizeRemoval(ctx, r)
	}

	// Root still present: check whether the removal transaction failed and
	// needs rescheduling.
	if r.RemoveMessageHash != nil {
		var wait models.MessageWaitsEth
		err := p.db.WithContext(ctx).
			Where("signed_tx_hash = ?", *r.RemoveMessageHash).
			First(&wait).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("checking removal tx status: %w", err)
		}
		if err == nil && wait.TxStatus == "confirmed" && wait.TxSuccess != nil && !*wait.TxSuccess {
			log.Warnw("piece removal transaction failed, rescheduling",
				"blob", multihash.Multihash(r.Blob), "tx", *r.RemoveMessageHash)
			return p.db.WithContext(ctx).
				Model(&models.PDPPendingRemoval{}).
				Where("blob = ?", r.Blob).
				Updates(map[string]any{
					"state":               models.PendingRemovalStatePending,
					"remove_message_hash": nil,
				}).Error
		}
	}
	return nil
}

// finalizeRemoval deletes the blob's bytes and every bookkeeping row tied to
// its piece: parked_pieces (cascading parked_piece_refs and pdp_piecerefs),
// the mhash→commp mapping, and the pending-removal row itself.
func (p *PDPService) finalizeRemoval(ctx context.Context, r models.PDPPendingRemoval) error {
	blob := multihash.Multihash(r.Blob)
	if err := p.blobstore.Delete(ctx, blob); err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("deleting blob bytes: %w", err)
	}
	if err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("piece_cid = ?", r.Commp).Delete(&models.ParkedPiece{}).Error; err != nil {
			return fmt.Errorf("deleting parked piece: %w", err)
		}
		if err := tx.Where("piece_cid = ?", r.Commp).Delete(&models.PDPPieceRef{}).Error; err != nil {
			return fmt.Errorf("deleting piece refs: %w", err)
		}
		if err := tx.Where("mhash = ?", []byte(blob)).Delete(&models.PDPPieceMHToCommp{}).Error; err != nil {
			return fmt.Errorf("deleting mhash to commp mapping: %w", err)
		}
		if err := tx.Where("blob = ?", r.Blob).Delete(&models.PDPPendingRemoval{}).Error; err != nil {
			return fmt.Errorf("deleting pending removal row: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("finalizing removal of blob %s: %w", blob, err)
	}
	log.Infow("finalized piece removal", "blob", blob, "commp", r.Commp)
	return nil
}
