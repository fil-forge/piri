package testutil

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/principal"
	utestutil "github.com/fil-forge/ucantone/testutil"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config/app"
)

// TestConfigOption is a function that modifies a test config
type TestConfigOption func(*testing.T, *app.AppConfig)

// NewTestConfig creates a new test config with sensible defaults
// This follows the functional options pattern for easy customization
func NewTestConfig(t *testing.T, opts ...TestConfigOption) app.AppConfig {
	t.Helper()

	// Get an OS-assigned port to avoid conflicts in parallel tests
	port := GetFreePort(t)
	publicURL, err := url.Parse(fmt.Sprintf("http://localhost:%d", port))
	require.NoError(t, err)

	// Start with sensible defaults for testing
	cfg := app.AppConfig{
		Identity: app.IdentityConfig{
			Signer: utestutil.RandomSigner(t), // per-test random signer
		},
		Server: app.ServerConfig{
			Host:      "localhost",
			Port:      uint(port),
			PublicURL: *publicURL,
		},
		Storage: app.StorageConfig{
			DataDir: "", // Empty = memory stores by default
			TempDir: "",
		},
		UCANService: app.UCANServiceConfig{
			Services: app.ExternalServicesConfig{
				// Upload.Connection is intentionally left zero until Phase 5
				// migrates the service config types to ucantone.
				Publisher: app.PublisherServiceConfig{
					PublicMaddr:   mustMaddr(t, fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/http", port)),
					AnnounceMaddr: mustMaddr(t, fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/http", port)),
					AnnounceURLs:  []url.URL{}, // Empty by default for tests
				},
			},
			ProofSetID: 0,
		},
		Replicator: app.DefaultReplicatorConfig(),
	}

	// Apply all options
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

// WithUploadServiceConfig sets the upload service DID on the test config.
// The Client field is left nil — handlers that only read the DID (e.g.
// access/grant) don't need a working client; tests exercising flows that
// dispatch back to the upload service must wire one explicitly.
func WithUploadServiceConfig(uploadDID did.DID, _ *url.URL) TestConfigOption {
	return func(_ *testing.T, cfg *app.AppConfig) {
		cfg.UCANService.Services.Upload.DID = uploadDID
	}
}

func mustMaddr(t *testing.T, s string) multiaddr.Multiaddr {
	t.Helper()
	m, err := multiaddr.NewMultiaddr(s)
	require.NoError(t, err)
	return m
}
