package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fil-forge/ucantone/ucan"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/hashicorp/go-multierror"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

// discardedUploadAge is how long ago an upload must have been discarded
// before the expiry task drops its data and its row: by then it has completed
// or stopped.
const discardedUploadAge = time.Hour

// ProcessExpiredAllocations reaps allocations made without a digest once they
// expire. A crash before the `/blob/add` receipt reaches the client leaves it
// nothing to abort, so without this the upload row, the staged data and the
// claim on (digest, space) would stay forever. An expired allocation that was
// accepted only loses its pending record: the acceptance carries the claim
// from then on. The bytes of a released claim are queued for removal, which
// re-checks every claim before deleting anything.
func (p *PDPService) ProcessExpiredAllocations(ctx context.Context) error {
	var expired []allocation.Pending
	for pending, err := range p.allocationStore.ListExpiredPending(ctx, ucan.Now()) {
		if err != nil {
			return fmt.Errorf("listing expired allocations: %w", err)
		}
		expired = append(expired, pending)
	}

	var merr *multierror.Error
	for _, pending := range expired {
		if err := p.expireAllocation(ctx, pending); err != nil {
			merr = multierror.Append(merr, fmt.Errorf("expiring allocation %s: %w", pending.Allocation, err))
		}
	}
	if err := p.reapDiscardedUploads(ctx); err != nil {
		merr = multierror.Append(merr, err)
	}
	return merr.ErrorOrNil()
}

// expireAllocation expires one allocation in its own transaction, so one
// that fails leaves the others expired. The allocation is locked and read
// again, since its upload may have recorded a digest since it was listed;
// then the space's claim on a received blob is locked before its acceptance
// is read, as an accept locks it. The upload's data is dropped once the
// expiry commits.
func (p *PDPService) expireAllocation(ctx context.Context, listed allocation.Pending) error {
	return harmonytx.Run(ctx, p.db, func(ctx context.Context, _ *harmonydb.Tx) error {
		if err := p.allocationStore.LockPending(ctx, listed.Allocation); err != nil {
			return err
		}
		pending, err := p.allocationStore.GetPending(ctx, listed.Allocation)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(pending.Digest) > 0 {
			if err := p.allocationStore.Lock(ctx, pending.Digest, pending.Space); err != nil {
				return err
			}
		}
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
		released, err := p.allocationStore.ReleasePending(ctx, pending)
		if err != nil {
			return err
		}
		if len(released.Digest) > 0 {
			if err := p.RemovePiece(ctx, released.Digest); err != nil {
				return err
			}
		}
		return harmonytx.AfterCommit(ctx, func(context.Context) error {
			log.Infow("expired allocation", "allocation", pending.Allocation, "space", pending.Space)
			return nil
		})
	})
}

// reapDiscardedUploads drops the data and the rows of uploads discarded at
// least discardedUploadAge ago. Their upload has completed or stopped by then:
// one that completed after its discard already dropped both, so a row left is
// one whose upload stopped before completing, and whose data, if it wrote
// any, nothing else refers to.
func (p *PDPService) reapDiscardedUploads(ctx context.Context) error {
	var rows []struct {
		ID string `db:"id"`
	}
	if err := p.db.Select(ctx, &rows,
		`SELECT id FROM pdp_piece_uploads WHERE discarded_at < $1`,
		time.Now().Add(-discardedUploadAge)); err != nil {
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
