package manager

import (
	"context"
	"fmt"
	"sync"

	"github.com/fil-forge/go-libstoracha/ipnipublisher/store"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/internal/ipldstore"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/aatodo_types"
)

// BufferStore provides persistent storage for submission state
type BufferStore interface {
	// Aggregation retrieves the pending pieces aggregation.
	Aggregation(context.Context) (aatodo_types.Aggregation, error)
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
	store   ipldstore.KVStore[aggBufferKey, aatodo_types.Aggregation]
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
		store: ipldstore.CBORStore[aggBufferKey, aatodo_types.Aggregation](ss),
	}

	// Initialize empty buffer at creation time to avoid race conditions
	// and side effects in read operations
	ctx := context.Background()
	if err := sw.store.Put(ctx, aggBufferKey{}, aatodo_types.Aggregation{Roots: []cid.Cid{}}); err != nil {
		return nil, fmt.Errorf("putting empty buffer: %w", err)
	}

	return sw, nil
}

// Aggregation retrieves the current submission buffer state
func (sw *submissionWorkspace) Aggregation(ctx context.Context) (aatodo_types.Aggregation, error) {
	sw.storeMu.RLock()
	defer sw.storeMu.RUnlock()

	buf, err := sw.store.Get(ctx, aggBufferKey{})
	if err != nil {
		// If not found, return empty aggregation (should not happen after initialization)
		if store.IsNotFound(err) {
			return aatodo_types.Aggregation{Roots: []cid.Cid{}}, nil
		}
		return aatodo_types.Aggregation{}, fmt.Errorf("reading submission buffer: %w", err)
	}
	return buf, nil
}

// AppendRoots atomically appends new roots to the buffer
func (sw *submissionWorkspace) AppendRoots(ctx context.Context, roots []cid.Cid) error {
	if len(roots) == 0 {
		return nil
	}

	sw.storeMu.Lock()
	defer sw.storeMu.Unlock()

	buffer, err := sw.store.Get(ctx, aggBufferKey{})
	if err != nil {
		return fmt.Errorf("getting buffer for append: %w", err)
	}
	buffer.Roots = append(buffer.Roots, roots...)

	if err := sw.store.Put(ctx, aggBufferKey{}, buffer); err != nil {
		return fmt.Errorf("saving buffer after append: %w", err)
	}

	return nil
}

// ClearRoots atomically clears the pending roots
func (sw *submissionWorkspace) ClearRoots(ctx context.Context) error {
	sw.storeMu.Lock()
	defer sw.storeMu.Unlock()

	return sw.store.Put(ctx, aggBufferKey{}, aatodo_types.Aggregation{Roots: []cid.Cid{}})
}
