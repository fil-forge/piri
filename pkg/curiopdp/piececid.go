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
			if _, err := tx.Exec(`UPDATE parked_pieces SET piece_cid = $2 WHERE piece_cid = $1`, v2, v1); err != nil {
				return false, fmt.Errorf("rewriting parked piece %s: %w", v2, err)
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
