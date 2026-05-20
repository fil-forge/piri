package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fil-forge/go-libstoracha/ipnipublisher/store"
	"github.com/fil-forge/go-libstoracha/metadata"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/sync"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/blobstore"
	"github.com/fil-forge/piri/pkg/store/consolidationstore"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
	"github.com/fil-forge/piri/pkg/store/local/keystore"
	"github.com/fil-forge/piri/pkg/store/local/retrievaljournal"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

var Module = fx.Module("memory-store",
	fx.Provide(
		fx.Annotate(
			NewAggregatorDatastore,
			// tagged as aggregator_datastore since this returns a datastore.Datastore which is too generic to
			// safely provide as is.
			fx.ResultTags(`name:"aggregator_datastore"`),
		),
		fx.Annotate(
			NewPublisherStore,
			// provide as FullStore (self)
			fx.As(fx.Self()),
			// provide sub-interfaces of Full Store
			fx.As(new(store.PublisherStore)),
			fx.As(new(store.EncodeableStore)),
		),
		NewAllocationStore,
		NewAcceptanceStore,
		NewClaimStore,
		NewReceiptStore,
		NewRetrievalJournal,
		NewKeyStore,
		NewConsolidationStore,
		NewPDPStore,
	),
)

func NewAggregatorDatastore() datastore.Datastore {
	return sync.MutexWrap(datastore.NewMapDatastore())
}

func NewAllocationStore() allocationstore.AllocationStore {
	ds := sync.MutexWrap(datastore.NewMapDatastore())
	return allocationstore.NewDatastoreStore(ds)
}

func NewAcceptanceStore() acceptancestore.AcceptanceStore {
	ds := sync.MutexWrap(datastore.NewMapDatastore())
	return acceptancestore.NewDatastoreStore(ds)
}

func NewClaimStore() invocationstore.InvocationStore {
	ds := sync.MutexWrap(datastore.NewMapDatastore())
	return invocationstore.NewDatastoreStore(ds)
}

func NewPublisherStore() store.FullStore {
	ds := sync.MutexWrap(datastore.NewMapDatastore())
	return store.FromDatastore(ds, store.WithMetadataContext(metadata.MetadataContext))
}

func NewReceiptStore() receiptstore.ReceiptStore {
	ds := sync.MutexWrap(datastore.NewMapDatastore())
	return receiptstore.NewDatastoreStore(ds)
}

// TODO need an in-memory impl of the retrieval journal...
func NewRetrievalJournal(lc fx.Lifecycle) (retrievaljournal.Journal, error) {
	tmpDir := filepath.Join(os.TempDir(), "piri-retrieval-journal-tmp")
	rj, err := retrievaljournal.NewFSJournal(tmpDir, 0)
	if err != nil {
		return nil, fmt.Errorf("creating retrieval journal: %w", err)
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return rj.Close()
		},
	})

	return rj, nil
}

func NewKeyStore() (keystore.KeyStore, error) {
	ds := sync.MutexWrap(datastore.NewMapDatastore())
	return keystore.NewKeyStore(ds)
}

func NewPDPStore() blobstore.Blobstore {
	return blobstore.NewDatastoreStore(sync.MutexWrap(datastore.NewMapDatastore()))
}

func NewConsolidationStore() consolidationstore.Store {
	ds := sync.MutexWrap(datastore.NewMapDatastore())
	return consolidationstore.NewDatastoreStore(ds)
}
