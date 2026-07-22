package service

import (
	"context"
	"fmt"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/google/uuid"

	"github.com/fil-forge/piri/pkg/pdp/types"
)

func (p *PDPService) AllocatePiece(ctx context.Context, allocation types.PieceAllocation) (res *types.AllocatedPiece, retErr error) {
	log.Infow("allocating piece", "request", allocation)
	defer func() {
		if retErr != nil {
			log.Errorw("failed to allocate piece", "request", allocation, "err", retErr)
		} else {
			log.Infow("allocated piece", "request", allocation, "response", res)
		}
	}()
	if abi.UnpaddedPieceSize(allocation.Piece.Size) > PieceSizeLimit {
		return nil, types.NewErrorf(types.KindInvalidInput, "piece size %d exceeds limit %d", allocation.Piece.Size, PieceSizeLimit)
	}

	// check if we already have this piece
	found, err := p.Has(ctx, allocation.Piece.Hash)
	if err != nil {
		return nil, types.WrapError(types.KindInternal, "failed to check if allocation exists", err)
	}
	if found {
		// if we have the piece, no allocation is required as we already have it.
		return &types.AllocatedPiece{
			Allocated: false,
			Piece:     allocation.Piece.Hash,
			UploadID:  uuid.Nil,
		}, nil

	}

	uploadUUID := uuid.New()

	notifyURL := ""
	if allocation.Notify != nil {
		notifyURL = allocation.Notify.String()
	}

	if _, err := p.db.Exec(ctx,
		`INSERT INTO pdp_piece_uploads (id, service, notify_url, check_hash_codec, check_hash, check_size)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		uploadUUID.String(), "storacha", notifyURL, allocation.Piece.Name, allocation.Piece.Hash, allocation.Piece.Size); err != nil {
		return nil, fmt.Errorf("failed to store upload request in database: %w", err)
	}

	return &types.AllocatedPiece{
		Allocated: true,
		Piece:     allocation.Piece.Hash,
		UploadID:  uploadUUID,
	}, nil

}
