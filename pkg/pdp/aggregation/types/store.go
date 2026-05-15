package types

import (
	"github.com/fil-forge/go-libstoracha/ipnipublisher/store"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/internal/ipldstore"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/aatodo_types"
)

// Store persists aggregates keyed by their root CID.
type Store ipldstore.KVStore[cid.Cid, aatodo_types.Aggregate]

type StoreParams struct {
	fx.In
	Datastore datastore.Datastore `name:"aggregator_datastore"`
}

const AggregatePrefix = "aggregates/"

func NewStore(params StoreParams) Store {
	ss := store.SimpleStoreFromDatastore(namespace.Wrap(params.Datastore, datastore.NewKey(AggregatePrefix)))
	return ipldstore.CBORStore[cid.Cid, aatodo_types.Aggregate](ss)
}
