package store

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/fx/store/filesystem"
	"github.com/fil-forge/piri/pkg/fx/store/memory"
	"github.com/fil-forge/piri/pkg/fx/store/s3"
)

// StorageModule returns the fx module for the configured object-store backend.
// S3 mode combines the s3 module (bulk stores) with filesystem.LocalOnlyModule
// for the four stores that must remain on disk regardless of backend.
func StorageModule(cfg app.ObjectStoreConfig) fx.Option {
	switch cfg.Type {
	case app.ObjectStoreTypeS3:
		return fx.Options(s3.Module, filesystem.LocalOnlyModule)
	case app.ObjectStoreTypeMemory:
		return memory.Module
	default:
		return filesystem.Module
	}
}
