// Package commp defines the aggregation pipeline's ingress seam. The
// implementation lives in pkg/pdp/pipeline (harmonytask-backed); this
// package keeps only the interface so consumers (/blob/accept, replica
// transfer) don't depend on the pipeline machinery.
package commp

import (
	"context"

	"github.com/multiformats/go-multihash"
)

// Calculator accepts blobs into the aggregation pipeline: commP
// calculation, aggregate folding, and on-chain root submission. Enqueue
// returns once the blob is durably recorded; the pipeline advances it
// asynchronously.
type Calculator interface {
	Enqueue(ctx context.Context, blob multihash.Multihash) error
}
