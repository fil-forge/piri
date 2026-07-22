package service

import (
	"context"
	"errors"
	"fmt"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/hashicorp/go-multierror"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	libpiece "github.com/fil-forge/libforge/piece"
	"github.com/fil-forge/piri/lib/verifyread"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/presets"
)

func (p *PDPService) UploadPiece(ctx context.Context, pieceUpload types.PieceUpload) (retErr error) {
	var checkHash []byte
	var checkSize int64
	var checkHashCodec string
	if err := p.db.QueryRow(ctx,
		`SELECT check_hash, check_size, check_hash_codec FROM pdp_piece_uploads WHERE id = $1`,
		pieceUpload.ID.String()).Scan(&checkHash, &checkSize, &checkHashCodec); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return types.NewErrorf(types.KindNotFound, "upload ID %s not found", pieceUpload.ID)
		}
		return types.WrapError(types.KindInternal, "failed to query for piece upload", err)
	}
	lg := log.With("upload_id", pieceUpload.ID, "digest", multihash.Multihash(checkHash).String(), "size", checkSize)

	hasher, ok := presets.HasherRegistry[checkHashCodec]
	if !ok {
		return types.NewErrorf(types.KindInvalidInput, "unknown hash code: %s", checkHashCodec)
	}

	mh, err := multihash.Decode(checkHash)
	if err != nil {
		return types.WrapError(types.KindInternal, "failed to decode check hash", err)
	}

	vr, err := verifyread.New(pieceUpload.Data, hasher(), mh.Digest)
	if err != nil {
		return types.WrapError(types.KindInternal, "failed to create verification reader", err)
	}

	if err := p.blobstore.Put(ctx, checkHash, uint64(checkSize), vr); err != nil {
		lg.Errorw("failed to write upload to blobstore", "err", err)
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
