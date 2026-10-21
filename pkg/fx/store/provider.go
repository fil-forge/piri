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
	if cfg.S3 != nil && cfg.S3.Endpoint != "" && cfg.S3.BucketPrefix != "" {
		// LocalOnlyModule keeps the stores that must remain on the local
		// filesystem (PublisherStore, RetrievalJournal, KeyStore)
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
