package pdpfake

import (
	"context"
	"sync"

	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
)

// Commp is a no-op commp.Calculator. Enqueue records calls but does not
// compute or persist anything.
type Commp struct {
	mu     sync.Mutex
	queued []multihash.Multihash
}

// NewCommp returns an empty Commp fake.
func NewCommp() *Commp { return &Commp{} }

// Enqueue records the call and returns nil.
func (c *Commp) Enqueue(_ context.Context, blob multihash.Multihash) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queued = append(c.queued, blob)
	return nil
}

// Queued returns the multihashes Enqueue was called with, in order.
func (c *Commp) Queued() []multihash.Multihash {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]multihash.Multihash(nil), c.queued...)
}

var _ commp.Calculator = (*Commp)(nil)
