package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fil-forge/ucantone/ucan"
	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

// orphanUploadAge is how old an upload row for an allocation made without a
// digest must be before a missing pending record marks it orphaned. The
// allocate handler writes the pending record just after the row; only a node
// stopping between the two leaves a row without one.
const orphanUploadAge = time.Hour

// ProcessExpiredAllocations reaps allocations made without a digest once they
// expire. A crash before the `/blob/add` receipt reaches the client leaves it
// nothing to abort, so without this the upload row, the staged data and the
// claim on (digest, space) would stay forever. An expired allocation that was
// accepted only loses its pending record: the acceptance carries the claim
// from then on. The bytes of a released claim are queued for removal, which
// re-checks every claim before deleting anything.
func (p *PDPService) ProcessExpiredAllocations(ctx context.Context) error {
	now := ucan.Now()
	var expired []allocation.Pending
	for pending, err := range p.allocationStore.ListPending(ctx) {
		if err != nil {
			return fmt.Errorf("listing pending allocations: %w", err)
		}
		if pending.Expires <= now {
			expired = append(expired, pending)
		}
	}

	var merr *multierror.Error
	for _, pending := range expired {
		if err := p.expireAllocation(ctx, pending); err != nil {
			merr = multierror.Append(merr, fmt.Errorf("expiring allocation %s: %w", pending.Allocation, err))
		}
	}
	if err := p.discardOrphanUploads(ctx); err != nil {
		merr = multierror.Append(merr, err)
	}
	if err := p.reapDiscardedUploads(ctx); err != nil {
		merr = multierror.Append(merr, err)
	}
	return merr.ErrorOrNil()
}

func (p *PDPService) expireAllocation(ctx context.Context, pending allocation.Pending) error {
	accepted, err := acceptancestore.AcceptedAllocation(ctx, p.acceptanceStore, pending.Digest, pending.Space, pending.Allocation)
	if err != nil {
		return err
	}
	if accepted {
		return p.allocationStore.DeletePending(ctx, pending.Allocation)
	}
	if err := p.DiscardUpload(ctx, pending.UploadID); err != nil {
		return err
	}
	released, err := allocationstore.ReleasePending(ctx, p.allocationStore, pending)
	if err != nil {
		return err
	}
	if len(released.Digest) > 0 {
		if err := p.RemovePiece(ctx, released.Digest); err != nil {
			return err
		}
	}
	log.Infow("expired allocation", "allocation", pending.Allocation, "space", pending.Space)
	return nil
}

func (p *PDPService) discardOrphanUploads(ctx context.Context) error {
	var rows []struct {
		ID         string `db:"id"`
		Allocation string `db:"allocation"`
	}
	if err := p.db.Select(ctx, &rows,
		`SELECT id, allocation FROM pdp_piece_uploads
		 WHERE allocation IS NOT NULL AND discarded_at IS NULL AND created_at < $1`,
		time.Now().Add(-orphanUploadAge)); err != nil {
		return fmt.Errorf("listing uploads without a digest: %w", err)
	}
	var merr *multierror.Error
	for _, row := range rows {
		link, err := cid.Parse(row.Allocation)
		if err != nil {
			merr = multierror.Append(merr, fmt.Errorf("parsing allocation of upload %s: %w", row.ID, err))
			continue
		}
		if _, err := p.allocationStore.GetPending(ctx, link); err == nil {
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			merr = multierror.Append(merr, fmt.Errorf("getting allocation of upload %s: %w", row.ID, err))
			continue
		}
		if err := p.DiscardUpload(ctx, row.ID); err != nil {
			merr = multierror.Append(merr, err)
			continue
		}
		log.Infow("discarded orphaned upload", "upload_id", row.ID, "allocation", link)
	}
	return merr.ErrorOrNil()
}

// reapDiscardedUploads drops the data and the rows of uploads discarded at
// least orphanUploadAge ago. Their upload has completed or stopped by then:
// one that completed after its discard already dropped both, so a row left is
// one whose upload stopped before completing, and whose data, if it wrote
// any, nothing else refers to.
func (p *PDPService) reapDiscardedUploads(ctx context.Context) error {
	var rows []struct {
		ID string `db:"id"`
	}
	if err := p.db.Select(ctx, &rows,
		`SELECT id FROM pdp_piece_uploads WHERE discarded_at < $1`,
		time.Now().Add(-orphanUploadAge)); err != nil {
		return fmt.Errorf("listing discarded uploads: %w", err)
	}
	var merr *multierror.Error
	for _, row := range rows {
		id := row.ID
		if err := p.blobstore.Unstage(ctx, id); err != nil {
			merr = multierror.Append(merr, fmt.Errorf("dropping data of discarded upload %s: %w", id, err))
			continue
		}
		if _, err := p.db.Exec(ctx, `DELETE FROM pdp_piece_uploads WHERE id = $1`, id); err != nil {
			merr = multierror.Append(merr, fmt.Errorf("deleting discarded upload %s: %w", id, err))
			continue
		}
		log.Infow("reaped discarded upload", "upload_id", id)
	}
	return merr.ErrorOrNil()
}
