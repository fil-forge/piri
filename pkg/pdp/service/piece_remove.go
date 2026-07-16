package service

import (
	"context"
	"fmt"

	"github.com/multiformats/go-multihash"
)

// RemovePiece records a request to release a blob's bytes. It never deletes
// anything inline: point-in-time state (acceptances, aggregation progress,
// on-chain piece rows) races against the concurrently-advancing pipeline,
// so classification is deferred to the removal machinery, which re-derives
// everything per pass and only acts when it can prove nothing references
// the bytes. Idempotent.
func (p *PDPService) RemovePiece(ctx context.Context, blob multihash.Multihash) (retErr error) {
	log.Infow("queueing piece removal", "blob", blob.String())
	defer func() {
		if retErr != nil {
			log.Errorw("failed to queue piece removal", "blob", blob.String(), "err", retErr)
		}
	}()

	if _, err := p.db.Exec(ctx, `
		INSERT INTO pdp_pending_piece_removals (blob) VALUES ($1)
		ON CONFLICT (blob) DO NOTHING
	`, []byte(blob)); err != nil {
		return fmt.Errorf("recording pending piece removal: %w", err)
	}
	return nil
}
