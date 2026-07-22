package manager

import (
	"context"
	"fmt"
	"sync"

	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/internal/ipldstore"
)

// Aggregation is the persisted submission buffer — the list of
// aggregate root CIDs waiting to be batched into a chain submission.
// cborgen-generated marshalers live in cbor_gen.go.
type Aggregation struct {
	Roots []cid.Cid `cborgen:"roots"`
}

// BufferStore provides persistent storage for submission state
type BufferStore interface {
	// Aggregation retrieves the pending pieces aggregation.
	Aggregation(context.Context) (Aggregation, error)
	// AppendRoots adds roots to the pending aggregation
	AppendRoots(context.Context, []cid.Cid) error
	// ClearRoots removes all roots from the current aggregation.
	ClearRoots(context.Context) error
}

// aggBufferKey is used as the single key for storing submission state
type aggBufferKey struct{}

func (aggBufferKey) String() string { return "aggregate_buffer" }

type submissionWorkspace struct {
	storeMu sync.RWMutex
	store   ipldstore.KVStore[aggBufferKey, Aggregation]
}

type SubmissionWorkspaceParams struct {
	fx.In
	Datastore datastore.Datastore `name:"aggregator_datastore"`
}

const ManagerKey = "manager/"

// NewSubmissionWorkspace creates a new submission workspace backed by the provided store
func NewSubmissionWorkspace(params SubmissionWorkspaceParams) (BufferStore, error) {
	ss := store.SimpleStoreFromDatastore(namespace.Wrap(params.Datastore, datastore.NewKey(ManagerKey)))
	sw := &submissionWorkspace{
		store: ipldstore.CBORStore[aggBufferKey, Aggregation](ss),
	}

	// Initialize an empty buffer ONLY when none exists. Unconditionally
	// resetting here loses roots that were buffered (aggregate stored,
	// Submit()ed) but not yet flushed to a chain submission when the process
	// died — they would never be re-submitted, silently dropping data from
	// the proving pipeline. Verified by the smelt restart-recovery e2e:
	// kill piri between "aggregate create" and the manager poll flush, and
	// the aggregate must still reach AddRoots after restart.
	ctx := context.Background()
	if _, err := sw.store.Get(ctx, aggBufferKey{}); err != nil {
		if !store.IsNotFound(err) {
			return nil, fmt.Errorf("reading submission buffer: %w", err)
		}
		emptyBuffer := Aggregation{
			Roots: []cid.Cid{},
		}
		if err := sw.store.Put(ctx, aggBufferKey{}, emptyBuffer); err != nil {
			return nil, fmt.Errorf("putting empty buffer: %w", err)
		}
	}

	return sw, nil
}

// Aggregates retrieves the current submission buffer state
func (sw *submissionWorkspace) Aggregation(ctx context.Context) (Aggregation, error) {
	sw.storeMu.RLock()
	defer sw.storeMu.RUnlock()

	buf, err := sw.store.Get(ctx, aggBufferKey{})
	if err != nil {
		// If not found, return empty aggregates (should not happen after initialization)
		if store.IsNotFound(err) {
			return Aggregation{
				Roots: []cid.Cid{},
			}, nil
		}
		return Aggregation{}, fmt.Errorf("reading submission buffer: %w", err)
	}
	return buf, nil
}

// AppendAggregates atomically appends new aggregates to the buffer
func (sw *submissionWorkspace) AppendRoots(ctx context.Context, aggregates []cid.Cid) error {
	if len(aggregates) == 0 {
		return nil
	}

	sw.storeMu.Lock()
	defer sw.storeMu.Unlock()

	buffer, err := sw.store.Get(ctx, aggBufferKey{})
	if err != nil {
		return fmt.Errorf("getting buffer for append: %w", err)
	} else {
		// Append to existing buffer
		buffer.Roots = append(buffer.Roots, aggregates...)
	}

	if err := sw.store.Put(ctx, aggBufferKey{}, buffer); err != nil {
		return fmt.Errorf("saving buffer after append: %w", err)
	}

	return nil
}

// ClearAggregates atomically clears the pending aggregates while preserving other state
func (sw *submissionWorkspace) ClearRoots(ctx context.Context) error {
	sw.storeMu.Lock()
	defer sw.storeMu.Unlock()

	return sw.store.Put(ctx, aggBufferKey{}, Aggregation{
		Roots: []cid.Cid{},
	})
}
