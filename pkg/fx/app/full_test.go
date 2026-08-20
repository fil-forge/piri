package app_test

import (
	"testing"

	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	fxapp "github.com/fil-forge/piri/pkg/fx/app"
	"github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/stretchr/testify/require"
)

// storageVariants exercises each branch of store.StorageModule, which picks a
// different set of storage modules at wiring time based on the config. Missing
// a dependency in any branch is a developer error we want to catch here rather
// than at server startup.
var storageVariants = map[string]testutil.TestConfigOption{
	"in-memory stores": func(_ *testing.T, cfg *app.AppConfig) {
		cfg.Storage.DataDir = ""
		cfg.Storage.S3 = nil
	},
	"filesystem stores": func(t *testing.T, cfg *app.AppConfig) {
		cfg.Storage.DataDir = t.TempDir()
		cfg.Storage.S3 = nil
	},
	"s3 stores": func(t *testing.T, cfg *app.AppConfig) {
		cfg.Storage.DataDir = t.TempDir()
		cfg.Storage.S3 = &app.S3Config{
			Endpoint:     "localhost:9000",
			BucketPrefix: "piri-test-",
		}
	},
}

// TestFullServerModule_ValidateApp ensures the full piri server's dependency
// graph is complete and wireable. fx.ValidateApp validates the graph without
// running any constructors, so no network endpoints are dialed.
func TestFullServerModule_ValidateApp(t *testing.T) {
	for name, withStorage := range storageVariants {
		t.Run(name, func(t *testing.T) {
			cfg := testutil.NewTestConfig(t, withStorage)

			err := fx.ValidateApp(
				fx.NopLogger,
				fxapp.FullServerModule(cfg),
			)

			require.NoError(t, err)
		})
	}
}
