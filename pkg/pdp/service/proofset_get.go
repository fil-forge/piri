package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"

	"github.com/fil-forge/piri/pkg/pdp/types"
)

type ProofSet struct {
	ID                 int64
	Roots              []RootEntry
	NextChallengeEpoch int64
}

type RootEntry struct {
	RootID        uint64 `json:"rootId"`
	RootCID       string `json:"rootCid"`
	SubrootCID    string `json:"subrootCid"`
	SubrootOffset int64  `json:"subrootOffset"`
}

func (p *PDPService) GetProofSet(ctx context.Context, id uint64) (res *types.ProofSet, retErr error) {
	log.Infow("getting proof set", "id", id)
	defer func() {
		if retErr != nil {
			log.Errorw("failed to get proof set", "id", id, "err", retErr)
		} else {
			log.Infow("got proof set", "id", id, "response", res)
		}
	}()
	// Retrieve the data-set record.
	var ps struct {
		ID                        int64  `db:"id"`
		Service                   string `db:"service"`
		InitReady                 bool   `db:"init_ready"`
		ProveAtEpoch              *int64 `db:"prove_at_epoch"`
		PrevChallengeRequestEpoch *int64 `db:"prev_challenge_request_epoch"`
		ProvingPeriod             *int64 `db:"proving_period"`
		ChallengeWindow           *int64 `db:"challenge_window"`
	}
	err := p.db.QueryRow(ctx, `
		SELECT id, service, init_ready, prove_at_epoch,
		       prev_challenge_request_epoch, proving_period, challenge_window
		FROM pdp_data_sets WHERE id = $1
	`, id).Scan(&ps.ID, &ps.Service, &ps.InitReady, &ps.ProveAtEpoch,
		&ps.PrevChallengeRequestEpoch, &ps.ProvingPeriod, &ps.ChallengeWindow)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, types.NewErrorf(types.KindNotFound, "proof set %d not found", id)
		}
		return nil, fmt.Errorf("failed to retrieve proof set: %w", err)
	}

	if ps.Service != p.name {
		return nil, types.NewError(types.KindUnauthorized, "not authorized")
	}

	// Retrieve the pieces (roots) associated with the data set.
	var roots []struct {
		PieceID        int64  `db:"piece_id"`
		Piece          string `db:"piece"`
		SubPiece       string `db:"sub_piece"`
		SubPieceOffset int64  `db:"sub_piece_offset"`
	}
	err = p.db.Select(ctx, &roots, `
		SELECT piece_id, piece, sub_piece, sub_piece_offset
		FROM pdp_data_set_pieces
		WHERE data_set = $1
		ORDER BY piece_id, sub_piece_offset
	`, id)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve proof set roots: %w", err)
	}

	// Build the response.
	response := &types.ProofSet{
		ID:          uint64(ps.ID),
		Initialized: ps.InitReady,
	}
	for _, r := range roots {
		rootCid, err := cid.Decode(r.Piece)
		if err != nil {
			return nil, fmt.Errorf("failed to decode root cid %s for proof set %d: %w", r.Piece, ps.ID, err)
		}
		subrootCid, err := cid.Decode(r.SubPiece)
		if err != nil {
			return nil, fmt.Errorf("failed to decode subroot cid %s for proof set %d: %w", r.SubPiece, ps.ID, err)
		}
		response.Roots = append(response.Roots, types.RootEntry{
			RootID:        uint64(r.PieceID),
			RootCID:       rootCid,
			SubrootCID:    subrootCid,
			SubrootOffset: r.SubPieceOffset,
		})
	}
	if ps.ProveAtEpoch != nil {
		response.NextChallengeEpoch = *ps.ProveAtEpoch
	}
	if ps.PrevChallengeRequestEpoch != nil {
		response.PreviousChallengeEpoch = *ps.PrevChallengeRequestEpoch
	}
	if ps.ProvingPeriod != nil {
		response.ProvingPeriod = *ps.ProvingPeriod
	}
	if ps.ChallengeWindow != nil {
		response.ChallengeWindow = *ps.ChallengeWindow
	}

	return response, nil
}
