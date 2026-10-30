package curiopdp

import (
	"context"
	"fmt"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/commcidv2"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/ipfs/go-cid"
)

// convertParkedPiecesToV1 rewrites parked_pieces.piece_cid and
// pdp_piecerefs.piece_cid from a v2 piece CID to its v1 form. Piri parked
// pieces under v2 before it switched to v1, the form pdpv0's tasks parse
// these columns as. Rows already holding v1 are left alone, so it runs on
// every start.
//
// Two v2 CIDs that differ only in payload size, as for payloads that differ
// only in trailing zero bytes, share a v1 CID. parked_pieces allows one active
// row per (piece_cid, piece_padded_size, long_term), so a second such piece
// is folded into the first: its refs move to the first row and it is
// deleted. The refs then read the first row's raw size, which yields the same
// commitment.
func convertParkedPiecesToV1(ctx context.Context, db *harmonydb.DB) error {
	var stored []string
	if err := db.Select(ctx, &stored, `
		SELECT piece_cid FROM parked_pieces
		UNION
		SELECT piece_cid FROM pdp_piecerefs`); err != nil {
		return fmt.Errorf("listing parked piece cids: %w", err)
	}

	toV1 := make(map[string]string)
	for _, s := range stored {
		c, err := cid.Parse(s)
		if err != nil {
			return fmt.Errorf("parsing parked piece cid %s: %w", s, err)
		}
		if !commcidv2.IsPieceCidV2(c) {
			continue
		}
		v1, _, err := commcid.PieceCidV1FromV2(c)
		if err != nil {
			return fmt.Errorf("deriving v1 piece cid from %s: %w", s, err)
		}
		toV1[s] = v1.String()
	}
	if len(toV1) == 0 {
		return nil
	}

	_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		for v2, v1 := range toV1 {
			if err := convertParkedPiece(tx, v2, v1); err != nil {
				return false, err
			}
			if _, err := tx.Exec(`UPDATE pdp_piecerefs SET piece_cid = $2 WHERE piece_cid = $1`, v2, v1); err != nil {
				return false, fmt.Errorf("rewriting piece ref %s: %w", v2, err)
			}
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return fmt.Errorf("converting parked pieces to v1 piece cids: %w", err)
	}
	log.Infow("converted parked pieces to v1 piece cids", "pieces", len(toV1))
	return nil
}

// convertParkedPiece rewrites the parked_pieces rows of the v2 piece CID to
// v1. An active row is folded into an active v1 row with the same padded size
// and long_term when one exists, which may be a row this loop just rewrote.
func convertParkedPiece(tx *harmonydb.Tx, v2, v1 string) error {
	var ids []int64
	if err := tx.Select(&ids, `SELECT id FROM parked_pieces WHERE piece_cid = $1 ORDER BY id`, v2); err != nil {
		return fmt.Errorf("listing parked pieces %s: %w", v2, err)
	}
	for _, id := range ids {
		var keep []int64
		if err := tx.Select(&keep, `
			SELECT k.id FROM parked_pieces k JOIN parked_pieces p ON p.id = $1
			WHERE p.cleanup_task_id IS NULL AND k.cleanup_task_id IS NULL
			  AND k.piece_cid = $2 AND k.piece_padded_size = p.piece_padded_size
			  AND k.long_term = p.long_term`, id, v1); err != nil {
			return fmt.Errorf("finding parked piece %s: %w", v1, err)
		}
		if len(keep) == 0 {
			if _, err := tx.Exec(`UPDATE parked_pieces SET piece_cid = $2 WHERE id = $1`, id, v1); err != nil {
				return fmt.Errorf("rewriting parked piece %s: %w", v2, err)
			}
			continue
		}
		if _, err := tx.Exec(`UPDATE parked_piece_refs SET piece_id = $2 WHERE piece_id = $1`, id, keep[0]); err != nil {
			return fmt.Errorf("moving refs of parked piece %s: %w", v2, err)
		}
		if _, err := tx.Exec(`DELETE FROM parked_pieces WHERE id = $1`, id); err != nil {
			return fmt.Errorf("deleting folded parked piece %s: %w", v2, err)
		}
	}
	return nil
}
