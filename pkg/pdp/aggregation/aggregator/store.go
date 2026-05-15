package aggregator

import (
	"context"
	"errors"

	"github.com/fil-forge/go-libstoracha/ipnipublisher/store"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"

	"github.com/fil-forge/piri/internal/ipldstore"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/aatodo_types"
)

type InProgressWorkspace interface {
	GetBuffer(context.Context) (aatodo_types.Buffer, error)
	PutBuffer(context.Context, aatodo_types.Buffer) error
}

type bufferKey struct{}

func (bufferKey) String() string { return "buffer" }

type inProgressWorkSpace struct {
	store ipldstore.KVStore[bufferKey, aatodo_types.Buffer]
}

func (i *inProgressWorkSpace) GetBuffer(ctx context.Context) (aatodo_types.Buffer, error) {
	buf, err := i.store.Get(ctx, bufferKey{})
	if errors.Is(err, datastore.ErrNotFound) || store.IsNotFound(err) {
		err := i.store.Put(ctx, bufferKey{}, aatodo_types.Buffer{})
		return aatodo_types.Buffer{}, err
	}
	return buf, err
}

func (i *inProgressWorkSpace) PutBuffer(ctx context.Context, buffer aatodo_types.Buffer) error {
	return i.store.Put(ctx, bufferKey{}, buffer)
}

const WorkspaceKey = "workspace/"

func newInProgressWorkspace(ds datastore.Datastore) InProgressWorkspace {
	ss := store.SimpleStoreFromDatastore(namespace.Wrap(ds, datastore.NewKey(WorkspaceKey)))
	return &inProgressWorkSpace{
		ipldstore.CBORStore[bufferKey, aatodo_types.Buffer](ss),
	}
}
