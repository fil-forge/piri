package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	leveldb "github.com/ipfs/go-ds-leveldb"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/store/blobstore"
	"github.com/fil-forge/piri/pkg/store/local/keystore"
	"github.com/fil-forge/piri/pkg/store/local/retrievaljournal"
	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/objectstore/flatfs"
)

// Module provides the stores backed by the local filesystem: blobs and the
// local-only stores. Use it alongside postgres.Module, which provides the
// record stores.
var Module = fx.Module("filesystem-store",
	fx.Provide(
		ProvideConfigs,
		fx.Annotate(
			NewIPNIObjectStore,
			fx.ResultTags(`name:"ipni_objectstore"`),
		),
		NewRetrievalJournal,
		NewKeyStore,
		NewPDPStore,
	),
)

// KeyStoreModule provides only the KeyStore, backed by filesystem.
// Use this with s3.Module when S3 is configured (KeyStore must always be on disk).
var KeyStoreModule = fx.Module("filesystem-keystore",
	fx.Provide(NewKeyStore),
)

// LocalOnlyModule provides stores that must always be local (filesystem-based).
// These stores cannot be backed by S3 due to their usage patterns:
// - RetrievalJournal: periodic filesystem-based journal with GC
// - KeyStore: private keys must never leave disk
//
// Use this module alongside s3.Module when S3 is configured.
var LocalOnlyModule = fx.Module("local-only-store",
	fx.Provide(
		ProvideLocalOnlyConfigs,
		NewRetrievalJournal,
		NewKeyStore,
	),
)

// LocalOnlyConfigs provides configs needed by LocalOnlyModule stores.
type LocalOnlyConfigs struct {
	fx.Out
	EgressTracker app.EgressTrackerStorageConfig
	KeyStore      app.KeyStoreConfig
}

// ProvideLocalOnlyConfigs extracts configs for local-only stores.
func ProvideLocalOnlyConfigs(cfg app.StorageConfig) LocalOnlyConfigs {
	return LocalOnlyConfigs{
		EgressTracker: cfg.EgressTracker,
		KeyStore:      cfg.KeyStore,
	}
}

type Configs struct {
	fx.Out
	Publisher     app.PublisherStorageConfig
	Blob          app.BlobStorageConfig
	EgressTracker app.EgressTrackerStorageConfig
	KeyStore      app.KeyStoreConfig
	Stash         app.StashStoreConfig
	PDP           app.PDPStoreConfig
}

// ProvideConfigs provides the fields of a storage config
func ProvideConfigs(cfg app.StorageConfig) Configs {
	return Configs{
		Publisher:     cfg.Publisher,
		Blob:          cfg.Blobs,
		EgressTracker: cfg.EgressTracker,
		KeyStore:      cfg.KeyStore,
		Stash:         cfg.StashStore,
		PDP:           cfg.PDPStore,
	}
}

// NewIPNIObjectStore provides the flatfs directory holding the node's IPNI
// adverts, one file per advert named by its CID. postgres.Module keeps the
// rest of the advertisement chain.
func NewIPNIObjectStore(cfg app.PublisherStorageConfig, lc fx.Lifecycle) (objectstore.Store, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("no data dir provided for IPNI object store")
	}
	if _, err := mkdirp(cfg.Dir); err != nil {
		return nil, fmt.Errorf("creating IPNI object store: %w", err)
	}
	objStore, err := flatfs.New(cfg.Dir, flatfs.NextToLast(2), false)
	if err != nil {
		return nil, fmt.Errorf("creating IPNI object store: %w", err)
	}
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return objStore.Close()
		},
	})
	return objectstore.Traced("ipni", objStore), nil
}

func NewRetrievalJournal(storeCfg app.EgressTrackerStorageConfig, svcCfg app.UCANServiceConfig, lc fx.Lifecycle) (retrievaljournal.Journal, error) {
	if storeCfg.Dir == "" {
		return nil, fmt.Errorf("no data dir provided for retrieval journal")
	}

	rj, err := retrievaljournal.NewFSJournal(storeCfg.Dir, svcCfg.Services.EgressTracker.MaxBatchSizeBytes)
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

func NewKeyStore(cfg app.KeyStoreConfig, lc fx.Lifecycle) (keystore.KeyStore, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("no data dir provided for key store")
	}

	ds, err := newDs(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("creating key store: %w", err)
	}
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return ds.Close()
		},
	})
	return keystore.NewKeyStore(ds)
}

func NewPDPStore(cfg app.PDPStoreConfig, lc fx.Lifecycle) (*blobstore.Store, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("no data dir provided for pdp store")
	}
	objStore, err := flatfs.New(cfg.Dir, flatfs.NextToLast(2), false)
	if err != nil {
		return nil, fmt.Errorf("creating pdp object store: %w", err)
	}
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return objStore.Close()
		},
	})
	return blobstore.NewFlatfsStore(objStore), nil
}

func newDs(path string) (*leveldb.Datastore, error) {
	dirPath, err := mkdirp(path)
	if err != nil {
		return nil, fmt.Errorf("creating leveldb for store at path %s: %w", path, err)
	}
	return leveldb.NewDatastore(dirPath, nil)
}

func mkdirp(dirpath ...string) (string, error) {
	dir := filepath.Join(dirpath...)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return "", fmt.Errorf("creating directory: %s: %w", dir, err)
	}
	return dir, nil
}
