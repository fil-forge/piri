package config

import (
	"fmt"
	"net/url"
	"time"

	"github.com/fil-forge/ucantone/client"
	"github.com/fil-forge/ucantone/did"
	"github.com/ipni/go-libipni/maurl"

	"github.com/fil-forge/piri/lib"
	"github.com/fil-forge/piri/pkg/config/app"
)

// buildServiceConnection constructs a piri ServiceConnection from a
// service DID and URL. It performs the URL+DID parse and wires up a
// ucantone HTTP client. Returns nil if didStr or urlStr is empty (so
// callers can keep optional services optional).
func buildServiceConnection(didStr, urlStr string) (app.ServiceConnection, error) {
	if didStr == "" || urlStr == "" {
		return app.ServiceConnection{}, fmt.Errorf("did and url are required")
	}
	d, err := did.Parse(didStr)
	if err != nil {
		return app.ServiceConnection{}, fmt.Errorf("parsing service DID %q: %w", didStr, err)
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return app.ServiceConnection{}, fmt.Errorf("parsing service URL %q: %w", urlStr, err)
	}
	c, err := client.NewHTTP(u)
	if err != nil {
		return app.ServiceConnection{}, fmt.Errorf("creating HTTP client for %q: %w", urlStr, err)
	}
	return app.ServiceConnection{DID: d, Client: c}, nil
}

type ServicesConfig struct {
	ServicePrincipalMapping map[string]string `mapstructure:"principal_mapping" flag:"service-principal-mapping" toml:"principal_mapping,omitempty"`

	Indexer       IndexingServiceConfig      `mapstructure:"indexer" validate:"required" toml:"indexer,omitempty"`
	EgressTracker EgressTrackerServiceConfig `mapstructure:"etracker" toml:"etracker,omitempty"`
	Upload        UploadServiceConfig        `mapstructure:"upload" validate:"required" toml:"upload,omitempty"`
	Publisher     PublisherServiceConfig     `mapstructure:"publisher" validate:"required" toml:"publisher,omitempty"`
}

func (s ServicesConfig) Validate() error {
	return validateConfig(s)
}

// Normalize applies compatibility fixes before validation.
func (s *ServicesConfig) Normalize() {
	// Compatibility shim: bump legacy sub-10MiB batch size to minimum.
	if s != nil && s.EgressTracker.MaxBatchSizeBytes > 0 && s.EgressTracker.MaxBatchSizeBytes < DefaultMinimumEgressBatchSize {
		log.Warnf("ucan.services.etracker.max_batch_size_bytes is below 10MiB (%d); overriding to %d for compatibility. Please update your config.", s.EgressTracker.MaxBatchSizeBytes, DefaultMinimumEgressBatchSize)
		s.EgressTracker.MaxBatchSizeBytes = DefaultMinimumEgressBatchSize
	}
}

func (s ServicesConfig) ToAppConfig(publicURL url.URL) (app.ExternalServicesConfig, error) {
	var (
		out app.ExternalServicesConfig
		err error
	)

	out.Upload, err = s.Upload.ToAppConfig()
	if err != nil {
		return app.ExternalServicesConfig{}, fmt.Errorf("creating upload service app config: %w", err)
	}
	out.Indexer, err = s.Indexer.ToAppConfig()
	if err != nil {
		return app.ExternalServicesConfig{}, fmt.Errorf("creating indexing service app config: %w", err)
	}
	out.EgressTracker, err = s.EgressTracker.ToAppConfig()
	if err != nil {
		return app.ExternalServicesConfig{}, fmt.Errorf("creating egress tracker service app config: %w", err)
	}

	out.Publisher, err = s.Publisher.ToAppConfig(publicURL)
	if err != nil {
		return app.ExternalServicesConfig{}, fmt.Errorf("creating publisher service app config: %w", err)
	}

	return out, nil
}

type IndexingServiceConfig struct {
	DID string `mapstructure:"did" validate:"required" flag:"indexing-service-did" toml:"did,omitempty"`
	URL string `mapstructure:"url" validate:"required,url" flag:"indexing-service-url" toml:"url,omitempty"`
}

func (s *IndexingServiceConfig) Validate() error {
	return validateConfig(s)
}

func (s *IndexingServiceConfig) ToAppConfig() (app.IndexingServiceConfig, error) {
	conn, err := buildServiceConnection(s.DID, s.URL)
	if err != nil {
		return app.IndexingServiceConfig{}, fmt.Errorf("creating index service app config: %w", err)
	}
	return app.IndexingServiceConfig{
		Connection: conn,
	}, nil
}

type EgressTrackerServiceConfig struct {
	DID              string `mapstructure:"did" flag:"egress-tracker-service-did" toml:"did,omitempty"`
	URL              string `mapstructure:"url" flag:"egress-tracker-service-url" toml:"url,omitempty"`
	ReceiptsEndpoint string `mapstructure:"receipts_endpoint" flag:"egress-tracker-service-receipts-endpoint" toml:"receipts_endpoint,omitempty"`
	// According to the spec, batch size should be between 10MiB and 1GiB
	// (see https://github.com/storacha/specs/blob/main/w3-egress-tracking.md)
	MaxBatchSizeBytes int64 `mapstructure:"max_batch_size_bytes" validate:"min=10485760,max=1073741824" flag:"egress-tracker-service-max-batch-size-bytes" toml:"max_batch_size_bytes,omitempty"`
}

func (c *EgressTrackerServiceConfig) Validate() error {
	return validateConfig(c)
}

func (c *EgressTrackerServiceConfig) ToAppConfig() (app.EgressTrackerServiceConfig, error) {
	if c.DID == "" {
		log.Warn("no egress tracker service DID provided, egress tracker is disabled")
		return app.EgressTrackerServiceConfig{}, nil
	}

	if c.URL == "" {
		log.Warn("no egress tracker service URL provided, egress tracker is disabled")
		return app.EgressTrackerServiceConfig{}, nil
	}

	conn, err := buildServiceConnection(c.DID, c.URL)
	if err != nil {
		return app.EgressTrackerServiceConfig{}, fmt.Errorf("creating egress tracker service connection: %w", err)
	}

	receiptsEndpoint, err := url.Parse(c.ReceiptsEndpoint)
	if err != nil {
		return app.EgressTrackerServiceConfig{}, fmt.Errorf("parsing egress tracker service receipts endpoint: %w", err)
	}

	return app.EgressTrackerServiceConfig{
		Connection:           conn,
		ReceiptsEndpoint:     receiptsEndpoint,
		MaxBatchSizeBytes:    c.MaxBatchSizeBytes,
		CleanupCheckInterval: 1 * time.Hour,
	}, nil
}

type UploadServiceConfig struct {
	DID string `mapstructure:"did" validate:"required" flag:"upload-service-did" toml:"did,omitempty"`
	URL string `mapstructure:"url" validate:"required,url" flag:"upload-service-url" toml:"url,omitempty"`
}

func (s *UploadServiceConfig) Validate() error {
	return validateConfig(s)
}

func (s *UploadServiceConfig) ToAppConfig() (app.UploadServiceConfig, error) {
	conn, err := buildServiceConnection(s.DID, s.URL)
	if err != nil {
		return app.UploadServiceConfig{}, fmt.Errorf("creating upload service connection: %w", err)
	}
	return app.UploadServiceConfig{Connection: conn}, nil
}

type PublisherServiceConfig struct {
	AnnounceURLs []string `mapstructure:"ipni_announce_urls" validate:"required,min=1,dive,url" flag:"ipni-announce-urls" toml:"ipni_announce_urls,omitempty"`
}

func (s *PublisherServiceConfig) Validate() error {
	return validateConfig(s)
}

func (s *PublisherServiceConfig) ToAppConfig(publicURL url.URL) (app.PublisherServiceConfig, error) {
	pubMaddr, err := maurl.FromURL(&publicURL)
	if err != nil {
		return app.PublisherServiceConfig{}, fmt.Errorf("converting public URL to multiaddr: %w", err)
	}

	// Parse IPNI announce URLs
	var announceURLs []url.URL
	for _, s := range s.AnnounceURLs {
		u, err := url.Parse(s)
		if err != nil {
			return app.PublisherServiceConfig{}, fmt.Errorf("parsing IPNI announce URL %s: %w", s, err)
		}
		announceURLs = append(announceURLs, *u)
	}

	pdpEndpoint, err := maurl.FromURL(&publicURL)
	if err != nil {
		return app.PublisherServiceConfig{}, fmt.Errorf("converting PDP URL to multiaddr: %w", err)
	}
	blobMaddr, err := lib.JoinHTTPPath(pdpEndpoint, "piece/{blobCID}")
	if err != nil {
		return app.PublisherServiceConfig{}, fmt.Errorf("creating blob multiaddr: %w", err)
	}
	return app.PublisherServiceConfig{
		PublicMaddr:   pubMaddr,
		AnnounceMaddr: pubMaddr,
		AnnounceURLs:  announceURLs,
		BlobMaddr:     blobMaddr,
	}, nil
}
