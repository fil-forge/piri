package store

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/fx/store/filesystem"
	"github.com/fil-forge/piri/pkg/fx/store/memory"
	"github.com/fil-forge/piri/pkg/fx/store/postgres"
	"github.com/fil-forge/piri/pkg/fx/store/s3"
)

// StorageModule returns the appropriate storage module based on configuration.
// Blobs go to S3 when it is configured, otherwise to the filesystem, and the
// records about them go to Postgres either way. With no data dir, every store
// is in memory.
func StorageModule(cfg app.StorageConfig) fx.Option {
	if s3Configured(cfg) {
		// LocalOnlyModule keeps the stores that must remain on the local
		// filesystem (AggregatorDatastore, RetrievalJournal, KeyStore)
		return fx.Options(
			s3.Module,
			filesystem.LocalOnlyModule,
			postgres.Module,
		)
	} else if cfg.DataDir == "" {
		return memory.Module
	}
	return fx.Options(
		filesystem.Module,
		postgres.Module,
	)
}

// PublisherStoreModule returns the module providing the IPNI advertisement
// chain store, which stays on the local filesystem even when blobs go to S3.
// With no data dir it is in memory. Compose it only while IPNI advertising
// is on.
func PublisherStoreModule(cfg app.StorageConfig) fx.Option {
	if !s3Configured(cfg) && cfg.DataDir == "" {
		return memory.PublisherStoreModule
	}
	return filesystem.PublisherStoreModule
}

func s3Configured(cfg app.StorageConfig) bool {
	return cfg.S3 != nil && cfg.S3.Endpoint != "" && cfg.S3.BucketPrefix != ""
}
