// Package commp defines the aggregation pipeline's ingress seam. The
// implementation lives in pkg/pdp/pipeline (harmonytask-backed); this
// package keeps only the interface so consumers (/blob/accept, replica
// transfer) don't depend on the pipeline machinery.
package commp

import (
	"context"
	"time"

	"github.com/multiformats/go-multihash"
)

// Calculator accepts blobs into the aggregation pipeline: commP
// calculation, aggregate folding, and on-chain root submission. Enqueue
// returns once the blob is durably recorded; the pipeline advances it
// asynchronously, after the delay the Policy sets.
type Calculator interface {
	Enqueue(ctx context.Context, blob multihash.Multihash) error
}

// DefaultDelay is how long an accepted blob waits before its commP is
// computed. A blob released inside the window costs no commP, parks no
// piece and adds no root that its removal would then retire. Published
// deletion-age traces put the bulk of deletions of fresh data in the first
// minutes and flatten within about an hour (the 90th percentile of the Yahoo
// HDFS traces falls between 22 and 75 minutes), so an hour covers nearly all
// of what a delay can save, at the cost of an hour added to time-to-first-proof.
const DefaultDelay = time.Hour

// MaxDelay bounds the configurable delay. Past it a blob spends longer unproven
// than any deletion pattern justifies.
const MaxDelay = 30 * 24 * time.Hour

// Policy reads the delay between a blob's acceptance and its commP task. The
// read function is called at every claim, so a change applies to blobs
// already waiting. The zero Policy has no delay.
type Policy struct {
	read func() time.Duration
}

// NewPolicy builds a Policy from a function returning the current delay.
func NewPolicy(read func() time.Duration) Policy {
	return Policy{read: read}
}

// Delay is how long an accepted blob waits before a commP task may claim it.
// Zero means a task is spawned at acceptance.
func (p Policy) Delay() time.Duration {
	if p.read == nil {
		return 0
	}
	if d := p.read(); d > 0 {
		return d
	}
	return 0
}
