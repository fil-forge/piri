package aggregator

import (
	"context"

	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/piri/internal/ipldstore"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/types"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"
)

type InProgressWorkspace interface {
	GetBuffer(context.Context) (types.Buffer, error)
	PutBuffer(context.Context, types.Buffer) error
}

type bufferKey struct{}

func (bufferKey) String() string { return "buffer" }

type inProgressWorkSpace struct {
	store ipldstore.KVStore[bufferKey, types.Buffer]
}

func (i *inProgressWorkSpace) GetBuffer(ctx context.Context) (types.Buffer, error) {
	buf, err := i.store.Get(ctx, bufferKey{})
	if store.IsNotFound(err) {
		err := i.store.Put(ctx, bufferKey{}, types.Buffer{})
		return types.Buffer{}, err
	}
	return buf, err
}

func (i *inProgressWorkSpace) PutBuffer(ctx context.Context, buffer types.Buffer) error {
	return i.store.Put(ctx, bufferKey{}, buffer)
}

const WorkspaceKey = "workspace/"

func newInProgressWorkspace(ds datastore.Datastore) InProgressWorkspace {
	ss := store.SimpleStoreFromDatastore(namespace.Wrap(ds, datastore.NewKey(WorkspaceKey)))
	return &inProgressWorkSpace{
		store: ipldstore.CBORStore[bufferKey, types.Buffer](ss),
	}
}
