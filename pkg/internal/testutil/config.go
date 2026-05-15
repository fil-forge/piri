package testutil

import (
	"fmt"
	"net/url"
	"testing"

	libforge_testutil "github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/principal"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config/app"
)

// TestConfigOption is a function that modifies a test config
type TestConfigOption func(*testing.T, *app.AppConfig)

// NewTestConfig creates a new test config with sensible defaults.
//
// NOTE: the Upload service connection is left nil during the UCAN 1.0
// migration. The `app.UploadServiceConfig.Connection` field is `any` until
// the ucantone outbound RPC client is wired into the config layer (Phase 6
// follow-up). Tests exercising outbound RPC must inject their own connection
// via WithUploadServiceConfig once that lands.
func NewTestConfig(t *testing.T, opts ...TestConfigOption) app.AppConfig {
	t.Helper()

	port := GetFreePort(t)
	publicURL, err := url.Parse(fmt.Sprintf("http://localhost:%d", port))
	require.NoError(t, err)

	cfg := app.AppConfig{
		Identity: app.IdentityConfig{
			Signer: libforge_testutil.Alice,
		},
		Server: app.ServerConfig{
			Host:      "localhost",
			Port:      uint(port),
			PublicURL: *publicURL,
		},
		Storage: app.StorageConfig{
			DataDir: "",
			TempDir: "",
		},
		UCANService: app.UCANServiceConfig{
			Services: app.ExternalServicesConfig{
				Upload: app.UploadServiceConfig{},
				Publisher: app.PublisherServiceConfig{
					PublicMaddr:   libforge_testutil.Must(multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/http", port)))(t),
					AnnounceMaddr: libforge_testutil.Must(multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/http", port)))(t),
					AnnounceURLs:  []url.URL{},
				},
			},
			ProofSetID: 0,
		},
		Replicator: app.DefaultReplicatorConfig(),
	}

	for _, opt := range opts {
		opt(t, &cfg)
	}

	return cfg
}

// WithSigner sets the identity signer
func WithSigner(signer principal.Signer) TestConfigOption {
	return func(_ *testing.T, cfg *app.AppConfig) {
		cfg.Identity.Signer = signer
	}
}

// WithUploadServiceConfig is a stub during the UCAN 1.0 migration. The
// `app.UploadServiceConfig.Connection` field is `any` until the ucantone
// outbound client is wired into the config layer. Callers needing to
// inject a real connection should override the field manually after
// NewTestConfig until that lands.
func WithUploadServiceConfig(_ did.DID, _ *url.URL) TestConfigOption {
	return func(_ *testing.T, _ *app.AppConfig) {}
}
