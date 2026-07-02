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
