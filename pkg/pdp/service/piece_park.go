package service

import (
	"context"
	"fmt"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/fil-forge/piri/pkg/pdp/types"
)

// ParkPiece records the piece under its v1 CID: parked_pieces and
// pdp_piecerefs are pdpv0 tables, and pdpv0's tasks parse their piece_cid as
// v1.
func (p *PDPService) ParkPiece(ctx context.Context, params types.ParkPieceRequest) error {
	pieceCID, err := asPieceCIDv1(params.PieceCID)
	if err != nil {
		return fmt.Errorf("deriving v1 piece cid from %s: %w", params.PieceCID, err)
	}
	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// 1. Create a long-term parked piece entry (marked complete since it's already in PDPStore).
		var pieceID int64
		if err := tx.QueryRow(
			`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, complete)
			 VALUES ($1, $2, $3, TRUE, TRUE) RETURNING id`,
			pieceCID.String(), int64(params.PaddedSize), int64(params.RawSize)).Scan(&pieceID); err != nil {
			return false, fmt.Errorf("failed to create parked_pieces entry: %w", err)
		}

		// 2. Create a parked piece ref pointing at PDPStore.
		dataURL := fmt.Sprintf("pdpstore://%s", params.Blob.String())
		var refID int64
		if err := tx.QueryRow(
			`INSERT INTO parked_piece_refs (piece_id, data_url, long_term, data_headers)
			 VALUES ($1, $2, TRUE, '{}'::jsonb) RETURNING ref_id`,
			pieceID, dataURL).Scan(&refID); err != nil {
			return false, fmt.Errorf("failed to create parked_piece_refs entry: %w", err)
		}

		// 3. Create a reference in pdp_piecerefs.
		if _, err := tx.Exec(
			`INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref) VALUES ($1, $2, $3)`,
			"storacha", pieceCID.String(), refID); err != nil {
			return false, fmt.Errorf("failed to create pdp_piecerefs entry: %w", err)
		}

		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to park piece: %w", err)
	}
	return nil
}
