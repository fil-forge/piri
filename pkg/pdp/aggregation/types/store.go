package types

import (
	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/internal/ipldstore"
)

type Store ipldstore.KVStore[cid.Cid, Aggregate]

type StoreParams struct {
	fx.In
	Datastore datastore.Datastore `name:"aggregator_datastore"`
}

const AggregatePrefix = "aggregates/"

func NewStore(params StoreParams) Store {
	ss := store.SimpleStoreFromDatastore(namespace.Wrap(params.Datastore, datastore.NewKey(AggregatePrefix)))
	return ipldstore.CBORStore[cid.Cid, Aggregate](ss)
}
