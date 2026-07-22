package service

import (
	"context"
	"fmt"

	"github.com/ipfs/go-cid"

	"github.com/fil-forge/piri/pkg/pdp/types"
)

func (p *PDPService) ListProofSets(ctx context.Context) (res []types.ProofSet, retErr error) {
	log.Info("listing proof sets")
	defer func() {
		if retErr != nil {
			log.Errorw("failed to list proof sets", "error", retErr)
		} else {
			log.Infow("listed proof sets", "count", len(res))
		}
	}()
	var dsets []struct {
		ID                        int64  `db:"id"`
		InitReady                 bool   `db:"init_ready"`
		ProveAtEpoch              *int64 `db:"prove_at_epoch"`
		PrevChallengeRequestEpoch *int64 `db:"prev_challenge_request_epoch"`
		ProvingPeriod             *int64 `db:"proving_period"`
		ChallengeWindow           *int64 `db:"challenge_window"`
	}
	err := p.db.Select(ctx, &dsets, `
		SELECT id, init_ready, prove_at_epoch, prev_challenge_request_epoch,
		       proving_period, challenge_window
		FROM pdp_data_sets WHERE service = $1
	`, p.name)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve proof sets: %w", err)
	}

	// Build the response for each proof set
	result := make([]types.ProofSet, 0, len(dsets))
	for _, proofSet := range dsets {
		// Retrieve the pieces (roots) associated with the data set
		var roots []struct {
			PieceID        int64  `db:"piece_id"`
			Piece          string `db:"piece"`
			SubPiece       string `db:"sub_piece"`
			SubPieceOffset int64  `db:"sub_piece_offset"`
		}
		err := p.db.Select(ctx, &roots, `
			SELECT piece_id, piece, sub_piece, sub_piece_offset
			FROM pdp_data_set_pieces
			WHERE data_set = $1
			ORDER BY piece_id, sub_piece_offset
		`, proofSet.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve proof set roots for proof set %d: %w", proofSet.ID, err)
		}

		// Build the response
		response := types.ProofSet{
			ID:          uint64(proofSet.ID),
			Initialized: proofSet.InitReady,
		}
		for _, r := range roots {
			rootCid, err := cid.Decode(r.Piece)
			if err != nil {
				return nil, fmt.Errorf("failed to decode root cid %s for proof set %d: %w", r.Piece, proofSet.ID, err)
			}
			subrootCid, err := cid.Decode(r.SubPiece)
			if err != nil {
				return nil, fmt.Errorf("failed to decode subroot cid %s for proof set %d: %w", r.SubPiece, proofSet.ID, err)
			}
			response.Roots = append(response.Roots, types.RootEntry{
				RootID:        uint64(r.PieceID),
				RootCID:       rootCid,
				SubrootCID:    subrootCid,
				SubrootOffset: r.SubPieceOffset,
			})
		}
		if proofSet.ProveAtEpoch != nil {
			response.NextChallengeEpoch = *proofSet.ProveAtEpoch
		}
		if proofSet.PrevChallengeRequestEpoch != nil {
			response.PreviousChallengeEpoch = *proofSet.PrevChallengeRequestEpoch
		}
		if proofSet.ProvingPeriod != nil {
			response.ProvingPeriod = *proofSet.ProvingPeriod
		}
		if proofSet.ChallengeWindow != nil {
			response.ChallengeWindow = *proofSet.ChallengeWindow
		}

		result = append(result, response)
	}

	return result, nil
}
