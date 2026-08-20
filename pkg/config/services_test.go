package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config/app"
)

func TestServicesConfig_Validate_IndexerDisabled(t *testing.T) {
	// A deployment without an indexing service (e.g. staging) omits the
	// indexer section and the IPNI announce URLs entirely.
	cfg := ServicesConfig{
		Upload: UploadServiceConfig{
			DID: "did:web:upload.example.com",
			URL: "https://upload.example.com",
		},
	}
	assert.NoError(t, cfg.Validate())
}

func TestIndexingServiceConfig_ToAppConfig_Disabled(t *testing.T) {
	disabledCases := map[string]IndexingServiceConfig{
		"empty config":    {},
		"DID without URL": {DID: "did:web:indexer.example.com"},
		"URL without DID": {URL: "https://indexer.example.com/claims"},
	}
	for desc, cfg := range disabledCases {
		t.Run(desc, func(t *testing.T) {
			result, err := cfg.ToAppConfig()
			require.NoError(t, err)
			assert.Equal(t, app.IndexingServiceConfig{}, result)
		})
	}
}

func TestPublisherServiceConfig_Validate_EmptyAnnounceURLs(t *testing.T) {
	cfg := PublisherServiceConfig{}
	assert.NoError(t, cfg.Validate())
}

// A zero max_batch_size_bytes means "use the default batch size" and must
// pass validation; egress tracking is disabled via empty DID/URL, not via
// this field.
func TestEgressTrackerServiceConfig_Validate_MaxBatchSizeBytes(t *testing.T) {
	cases := []struct {
		name    string
		size    int64
		wantErr bool
	}{
		{"zero selects the default", 0, false},
		{"spec minimum 10MiB", DefaultMinimumEgressBatchSize, false},
		{"spec maximum 1GiB", 1 << 30, false},
		{"positive below spec minimum", DefaultMinimumEgressBatchSize - 1, true},
		{"above spec maximum", 1<<30 + 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := EgressTrackerServiceConfig{MaxBatchSizeBytes: tc.size}
			err := cfg.Validate()
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestServicesConfig_Validate_ZeroEgressBatchSize(t *testing.T) {
	// The etracker section is valid without max_batch_size_bytes even when
	// the tracker itself is enabled; the default batch size applies.
	cfg := ServicesConfig{
		EgressTracker: EgressTrackerServiceConfig{
			DID: "did:web:etracker.example.com",
			URL: "https://etracker.example.com",
		},
		Upload: UploadServiceConfig{
			DID: "did:web:upload.example.com",
			URL: "https://upload.example.com",
		},
	}
	assert.NoError(t, cfg.Validate())
}
