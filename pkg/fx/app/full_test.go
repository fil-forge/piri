package app_test

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/fil-forge/go-ipni-tools/pkg/store"
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

// ipniVariants exercises both shapes of the IPNI advertising graph: with
// announce URLs the full server adds the advertisement chain store, its
// route and the publish task, and without them it leaves all three out.
var ipniVariants = map[string]testutil.TestConfigOption{
	"IPNI advertising on": func(_ *testing.T, cfg *app.AppConfig) {
		cfg.UCANService.Services.Publisher.AnnounceURLs = []url.URL{
			{Scheme: "https", Host: "ipni.example", Path: "/announce"},
		}
	},
	"IPNI advertising off": func(_ *testing.T, cfg *app.AppConfig) {
		cfg.UCANService.Services.Publisher.AnnounceURLs = nil
	},
}

// TestFullServerModule_ValidateApp ensures the full piri server's dependency
// graph is complete and wireable. fx.ValidateApp validates the graph without
// running any constructors, so no network endpoints are dialed.
func TestFullServerModule_ValidateApp(t *testing.T) {
	for storageName, withStorage := range storageVariants {
		for ipniName, withIPNI := range ipniVariants {
			t.Run(fmt.Sprintf("%s, %s", storageName, ipniName), func(t *testing.T) {
				cfg := testutil.NewTestConfig(t, withStorage, withIPNI)

				err := fx.ValidateApp(
					fx.NopLogger,
					fxapp.FullServerModule(cfg),
				)

				require.NoError(t, err)
			})
		}
	}
}

// TestFullServerModule_PublisherStoreOnlyWithIPNI ensures the advertisement
// chain store, a LevelDB under the publisher data dir, is in the graph only
// while IPNI advertising is on. Out of the graph, nothing can open it, so
// nothing is written under the publisher data dir.
func TestFullServerModule_PublisherStoreOnlyWithIPNI(t *testing.T) {
	for storageName, withStorage := range storageVariants {
		for ipniName, withIPNI := range ipniVariants {
			t.Run(fmt.Sprintf("%s, %s", storageName, ipniName), func(t *testing.T) {
				cfg := testutil.NewTestConfig(t, withStorage, withIPNI)

				err := fx.ValidateApp(
					fx.NopLogger,
					fxapp.FullServerModule(cfg),
					fx.Invoke(func(store.PublisherStore) {}),
				)

				if cfg.UCANService.Services.Publisher.IPNIEnabled() {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "missing type: store.PublisherStore")
				}
			})
		}
	}
}

// TestInitModule_ValidateApp ensures the graph `piri register` runs with,
// the PDP half without the UCAN services, is complete on its own. A provider
// placed in the PDP module that depends on a UCAN-side type passes the full
// server's validation and still fails the node at registration.
func TestInitModule_ValidateApp(t *testing.T) {
	for storageName, withStorage := range storageVariants {
		for ipniName, withIPNI := range ipniVariants {
			t.Run(fmt.Sprintf("%s, %s", storageName, ipniName), func(t *testing.T) {
				cfg := testutil.NewTestConfig(t, withStorage, withIPNI)

				err := fx.ValidateApp(
					fx.NopLogger,
					fxapp.InitModule(cfg),
				)

				require.NoError(t, err)
			})
		}
	}
}
