package service

import (
	"context"
	"errors"
	"fmt"
	"hash"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/digestutil"
	libpiece "github.com/fil-forge/libforge/piece"
	"github.com/fil-forge/piri/lib/verifyread"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/presets"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

// UploadPiece receives the data of an upload. The PUTs of one upload run one
// at a time: a client retrying a PUT that is still being received waits for
// it, then finds the upload complete and is refused, instead of completing
// the same upload twice and undoing the first.
func (p *PDPService) UploadPiece(ctx context.Context, pieceUpload types.PieceUpload) (retErr error) {
	defer p.uploadLocks.Lock(pieceUpload.ID.String())()
	var checkHash []byte
	var checkSize int64
	var checkHashCodec string
	var allocationLink *string
	if err := p.db.QueryRow(ctx,
		`SELECT check_hash, check_size, check_hash_codec, allocation FROM pdp_piece_uploads WHERE id = $1 AND discarded_at IS NULL`,
		pieceUpload.ID.String()).Scan(&checkHash, &checkSize, &checkHashCodec, &allocationLink); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return types.NewErrorf(types.KindNotFound, "upload ID %s not found", pieceUpload.ID)
		}
		return types.WrapError(types.KindInternal, "failed to query for piece upload", err)
	}
	lg := log.With("upload_id", pieceUpload.ID, "digest", multihash.Multihash(checkHash).String(), "size", checkSize)

	// Re-check the declared size against current policy. The allocation that
	// created this row passed the limit in force at the time; the operator
	// may have lowered it since, and an upload in flight across that change
	// must not slip through.
	if err := p.pieceSize.CheckRaw(uint64(checkSize)); err != nil {
		lg.Warnw("rejecting upload whose allocated size exceeds the current limit",
			"max", p.pieceSize.MaxRaw(), "err", err)
		return types.WrapError(types.KindPayloadTooLarge, "allocated piece exceeds the current size limit", err)
	}

	hasher, ok := presets.HasherRegistry[checkHashCodec]
	if !ok {
		return types.NewErrorf(types.KindInvalidInput, "unknown hash code: %s", checkHashCodec)
	}

	if len(checkHash) == 0 {
		if allocationLink == nil {
			return types.NewErrorf(types.KindInternal, "upload %s has neither a digest nor an allocation", pieceUpload.ID)
		}
		link, err := cid.Parse(*allocationLink)
		if err != nil {
			return types.WrapError(types.KindInternal, "failed to parse upload allocation", err)
		}
		return p.uploadUnhashedPiece(ctx, pieceUpload, uint64(checkSize), hasher, link)
	}

	mh, err := multihash.Decode(checkHash)
	if err != nil {
		return types.WrapError(types.KindInternal, "failed to decode check hash", err)
	}

	// Bound the body by the size the allocation declared, so an over-long
	// upload is cut off mid-stream instead of being written to the blobstore
	// in full and only then rejected by the digest compare.
	vr, err := verifyread.New(pieceUpload.Data, hasher(), mh.Digest,
		verifyread.WithExpectedSize(uint64(checkSize)))
	if err != nil {
		return types.WrapError(types.KindInternal, "failed to create verification reader", err)
	}

	if err := p.blobstore.Put(ctx, checkHash, uint64(checkSize), vr); err != nil {
		lg.Errorw("failed to write upload to blobstore", "err", err)
		if errors.Is(err, verifyread.ErrSizeMismatch) {
			return types.WrapError(types.KindPayloadTooLarge, "upload does not match its allocated size", err)
		}
		return types.WrapError(types.KindInvalidInput, "failed to put piece", err)
	}

	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// transaction since we only want to remove the upload entry if we can write to the store
		if _, err := tx.Exec(`DELETE FROM pdp_piece_uploads WHERE id = $1`, pieceUpload.ID.String()); err != nil {
			return false, types.WrapError(types.KindInternal, fmt.Sprintf("failed to delete piece upload ID %s from pdp_piece_uploads", pieceUpload.ID), err)
		}

		// if the upload was done with commp create a mapping for it now
		if checkHashCodec == multicodec.Fr32Sha256Trunc254Padbintree.String() {
			v2CID := libpiece.MultihashToCommpCID(checkHash)
			pv1, _, err := commcid.PieceCidV1FromV2(v2CID)
			if err != nil {
				return false, fmt.Errorf("failed to derive v1 piece CID from %s: %w", v2CID, err)
			}
			if _, err := tx.Exec(
				`INSERT INTO pdp_piece_mh_to_commp (mhash, size, commp, commp_v1) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
				checkHash, checkSize, v2CID.String(), pv1.String()); err != nil {
				return false, types.WrapError(types.KindInternal, "failed to create pieceMH to commp", err)
			}
		} else if checkHashCodec == multicodec.Sha2_256Trunc254Padded.String() {
			pv1, err := commcid.DataCommitmentV1ToCID(mh.Digest)
			if err != nil {
				return false, err
			}
			pieceCID, err := commcid.PieceCidV2FromV1(pv1, uint64(checkSize))
			if err != nil {
				return false, fmt.Errorf("failed to convert pieceCid %s from v1 to v2: %w", pv1, err)
			}
			if _, err := tx.Exec(
				`INSERT INTO pdp_piece_mh_to_commp (mhash, size, commp, commp_v1) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
				checkHash, checkSize, libpiece.MultihashToCommpCID(pieceCID.Hash()).String(), pv1.String()); err != nil {
				return false, types.WrapError(types.KindInternal, "failed to create pieceMH to commp", err)
			}
		}

		return true, nil
	})
	if err != nil {
		merr := new(multierror.Error)
		merr = multierror.Append(merr, err)

		lg.Errorw("failed to persist database records for piece upload", "err", err)
		// data is written to the blobstore before the metadata transaction; if the
		// transaction fails we must delete it from the blobstore.
		if delErr := p.blobstore.Delete(ctx, checkHash); delErr != nil {
			lg.Errorw("failed to delete data from blobstore for failed upload", "err", delErr)
			merr = multierror.Append(merr, delErr)
		}
		return merr.ErrorOrNil()
	}

	return nil
}

// uploadUnhashedPiece receives an upload whose allocation named only the hash
// function. The data is hashed as it is received and written under the
// upload's key, since its digest is not known until the last byte, and it
// stays there: once the blob is accepted, the settle task moves it to the key
// of its digest, before its commP is calculated. The steps run in an order
// that leaves nothing unclaimed if the node stops between any two of them:
//
//  1. the digest is recorded on the pending allocation;
//  2. the allocation is made to count as a claim on (digest, space), unless
//     one already does (another upload of the same content in the space);
//  3. the upload is recorded as holding the digest and its row deleted, in
//     one transaction; or, when the node already holds that content, the
//     upload's data is dropped and its row deleted.
//
// Until step 3 the upload can be retried, and a retry repeats every step. If
// the allocation is released while the upload completes, step 3 finds the
// row gone, and the upload undoes steps 1 and 2 and drops its data.
func (p *PDPService) uploadUnhashedPiece(ctx context.Context, pieceUpload types.PieceUpload, size uint64, hasher func() hash.Hash, link cid.Cid) error {
	lg := log.With("upload_id", pieceUpload.ID, "allocation", link, "size", size)
	pending, err := p.allocationStore.GetPending(ctx, link)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return types.NewErrorf(types.KindNotFound, "allocation %s for upload %s not found", link, pieceUpload.ID)
		}
		return types.WrapError(types.KindInternal, "failed to get pending allocation", err)
	}

	vr, err := verifyread.NewHashing(pieceUpload.Data, hasher(), verifyread.WithExpectedSize(size))
	if err != nil {
		return types.WrapError(types.KindInternal, "failed to create hashing reader", err)
	}
	id := pieceUpload.ID.String()
	if err := p.blobstore.Stage(ctx, id, size, vr); err != nil {
		lg.Errorw("failed to write upload to blobstore", "err", err)
		if delErr := p.blobstore.Unstage(ctx, id); delErr != nil {
			lg.Errorw("failed to delete data of failed upload", "err", delErr)
		}
		if errors.Is(err, verifyread.ErrSizeMismatch) {
			return types.WrapError(types.KindPayloadTooLarge, "upload does not match its allocated size", err)
		}
		return types.WrapError(types.KindInvalidInput, "failed to put piece", err)
	}
	sum, ok := vr.Sum()
	if !ok {
		return types.NewErrorf(types.KindInternal, "upload %s was stored without being read to the end", pieceUpload.ID)
	}
	encoded, err := multihash.Encode(sum, pending.DigestCode)
	if err != nil {
		return types.WrapError(types.KindInternal, "failed to encode computed digest", err)
	}
	digest := multihash.Multihash(encoded)
	lg = lg.With("digest", digest.String())

	pending.Digest = digest
	if err := p.allocationStore.PutPending(ctx, pending); err != nil {
		return types.WrapError(types.KindInternal, "failed to record computed digest", err)
	}
	if _, err := p.allocationStore.Claim(ctx, allocation.Allocation{
		Space:      pending.Space,
		Blob:       blob.Blob{Digest: digest, Size: size},
		Expires:    pending.Expires,
		Cause:      pending.Cause,
		Allocation: pending.Allocation,
	}); err != nil {
		return types.WrapError(types.KindInternal, "failed to record allocation for computed digest", err)
	}

	held, err := p.Has(ctx, digest)
	if err != nil {
		return types.WrapError(types.KindInternal, "failed to check for existing data", err)
	}
	// The upload completes by deleting its row. Discarding an upload marks the
	// same row first, so whichever gets to it first wins: an upload whose row
	// is marked was discarded while it completed, and records nothing.
	discarded := false
	if !held {
		// The upload holds the digest only if no other upload of the same
		// content claimed it first.
		held = true
		if _, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			n, err := tx.Exec(`DELETE FROM pdp_piece_uploads WHERE id = $1 AND discarded_at IS NULL`, id)
			if err != nil {
				return false, err
			}
			if n == 0 {
				discarded = true
				return false, nil
			}
			n, err = tx.Exec(`INSERT INTO pdp_staged_blobs (digest, upload_id) VALUES ($1, $2) ON CONFLICT (digest) DO NOTHING`, []byte(digest), id)
			if err != nil {
				return false, err
			}
			if n == 0 {
				return false, nil
			}
			held = false
			return true, nil
		}); err != nil {
			return types.WrapError(types.KindInternal, "failed to record the staged blob", err)
		}
	}
	if held && !discarded {
		// The data goes before the row, so a node stopping in between leaves
		// a row the expiry task reaps, never data nothing refers to.
		if err := p.blobstore.Unstage(ctx, id); err != nil {
			return types.WrapError(types.KindInternal, "failed to drop duplicate upload data", err)
		}
		n, err := p.db.Exec(ctx, `DELETE FROM pdp_piece_uploads WHERE id = $1 AND discarded_at IS NULL`, id)
		if err != nil {
			return types.WrapError(types.KindInternal, fmt.Sprintf("failed to delete piece upload ID %s from pdp_piece_uploads", pieceUpload.ID), err)
		}
		discarded = n == 0
	}
	if discarded {
		return p.releaseDiscardedUpload(ctx, id, pending)
	}
	lg.Infow("received upload without a digest", "blob", digestutil.Format(digest), "duplicate", held)
	return nil
}

// releaseDiscardedUpload undoes what an upload discarded while it completed
// recorded: its data, and the digest and claim on its pending allocation,
// which the release may have read before they were recorded. The claim's
// bytes, if any, are queued for removal, which re-checks every claim first.
// The upload writes nothing more, so its marked row goes once its data has.
func (p *PDPService) releaseDiscardedUpload(ctx context.Context, id string, pending allocation.Pending) error {
	if err := p.blobstore.Unstage(ctx, id); err != nil {
		return types.WrapError(types.KindInternal, "failed to drop data of discarded upload", err)
	}
	if _, err := p.db.Exec(ctx, `DELETE FROM pdp_piece_uploads WHERE id = $1`, id); err != nil {
		return types.WrapError(types.KindInternal, fmt.Sprintf("failed to delete discarded upload ID %s", id), err)
	}
	released, err := p.allocationStore.ReleasePending(ctx, pending)
	if err != nil {
		return types.WrapError(types.KindInternal, "failed to release allocation of discarded upload", err)
	}
	if len(released.Digest) > 0 {
		if err := p.RemovePiece(ctx, released.Digest); err != nil {
			return types.WrapError(types.KindInternal, "failed to queue removal of discarded upload", err)
		}
	}
	return types.NewErrorf(types.KindNotFound, "allocation %s was released while upload %s completed", pending.Allocation, id)
}

// DiscardUpload drops an upload that has not completed: whatever data it
// wrote, and its row once nothing can be writing more. A completed upload's
// data may hold a blob that other claims share, so it is left to the blob's
// removal.
//
// The row is marked first. An upload completing at the same time completes by
// deleting the row only while it is unmarked, so once it is marked here the
// upload can no longer record its data as a staged blob, and the check below
// cannot miss an entry recorded after it. The row stays, marked, because an
// upload still being written may write its data after this drops it: the
// upload drops it again when it completes, and the expiry task drops the data
// and the row of one that never does.
func (p *PDPService) DiscardUpload(ctx context.Context, uploadID string) error {
	if _, err := p.db.Exec(ctx, `
		UPDATE pdp_piece_uploads SET discarded_at = now() WHERE id = $1 AND discarded_at IS NULL
	`, uploadID); err != nil {
		return types.WrapError(types.KindInternal, fmt.Sprintf("failed to mark piece upload ID %s discarded", uploadID), err)
	}
	var holds bool
	if err := p.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pdp_staged_blobs WHERE upload_id = $1)`, uploadID).Scan(&holds); err != nil {
		return types.WrapError(types.KindInternal, fmt.Sprintf("failed to check whether upload %s holds a blob", uploadID), err)
	}
	if holds {
		return nil
	}
	if err := p.blobstore.Unstage(ctx, uploadID); err != nil {
		return types.WrapError(types.KindInternal, fmt.Sprintf("failed to delete data of upload %s", uploadID), err)
	}
	return nil
}
