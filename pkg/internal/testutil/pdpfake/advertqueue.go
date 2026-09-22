package pdpfake

import (
	"context"
	"sync"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/ipfs/go-cid"

	"github.com/fil-forge/piri/pkg/service/publisher"
	"github.com/fil-forge/piri/pkg/service/publisher/advert"
)

// AdvertQueue is an in-memory publisher.AdvertQueue. It records what was
// queued and dequeued and publishes nothing, which is what an fxtest app
// without a harmonydb needs.
type AdvertQueue struct {
	mu        sync.Mutex
	queued    []cid.Cid
	dequeued  []cid.Cid
	withdrawn []cid.Cid
}

// NewAdvertQueue returns an empty AdvertQueue fake.
func NewAdvertQueue() *AdvertQueue { return &AdvertQueue{} }

// Enqueue records the claim and returns nil.
func (q *AdvertQueue) Enqueue(_ context.Context, claim cid.Cid, _ advert.Spec) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.queued = append(q.queued, claim)
	return nil
}

// Claimed returns no rows: the fake queues nothing for a task to publish.
func (q *AdvertQueue) Claimed(context.Context, harmonytask.TaskID) ([]publisher.QueuedAdvert, error) {
	return nil, nil
}

// Dequeue records the claim and returns nil.
func (q *AdvertQueue) Dequeue(_ context.Context, claim cid.Cid) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dequeued = append(q.dequeued, claim)
	return nil
}

// Queued returns the claims Enqueue was called with, in order.
func (q *AdvertQueue) Queued() []cid.Cid {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]cid.Cid(nil), q.queued...)
}

// Withdraw records the claim and returns nil.
func (q *AdvertQueue) Withdraw(_ context.Context, claim cid.Cid) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.withdrawn = append(q.withdrawn, claim)
	return nil
}

// Withdrawn returns the claims Withdraw was called with, in order.
func (q *AdvertQueue) Withdrawn() []cid.Cid {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]cid.Cid(nil), q.withdrawn...)
}

// Dequeued returns the claims Dequeue was called with, in order.
func (q *AdvertQueue) Dequeued() []cid.Cid {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]cid.Cid(nil), q.dequeued...)
}

var (
	_ publisher.AdvertQueue = (*AdvertQueue)(nil)
	_ publisher.Withdrawer  = (*AdvertQueue)(nil)
)
