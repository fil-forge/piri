package config

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	"github.com/fil-forge/ucantone/client"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/ipni/go-libipni/maurl"

	"github.com/fil-forge/piri/lib"
	"github.com/fil-forge/piri/pkg/config/app"
)

// decodeProofChain base64-decodes a TOML-stored proof string into the
// delegation chain it encodes. The encoder side lives in
// cmd/cli/setup/register.go's encodeProofChain; both ends MUST agree on the
// encoding. The chain is logically ordered root → leaf (e.g.
// indexing-service → delegator → operator). All links must travel together
// when the operator invokes against the indexing/egress-tracker services —
// single-delegation storage was insufficient and produced "delegation issuer
// is did:web:indexer not did:web:delegator" errors in piri's publisher when
// only the leaf or only the root made it through.
//
// TODO(forrest)[ucan1]: remove orderProofChain once
// https://github.com/fil-forge/ucantone/issues/29 lands. The ucan-wg/container
// spec sorts tokens bytewise on encode for deterministic output (see
// ucantone/ucan/container/container.go encodeTokens), so ct.Delegations()
// returns them in bytewise order, not in chain order. The ucan-wg/invocation
// spec requires the invocation's `prf` field to be "an array of CIDs ...
// starting from the root Delegation ... in strict sequence where the aud of
// the previous Delegation matches the iss of the next Delegation" — so
// downstream consumers (publisher.go's CacheClaim) cannot just forward
// ct.Delegations() into WithProofs. We reorder here to bridge between the
// transport-layer container and the invocation-layer ordering requirement.
func decodeProofChain(s string) ([]ucan.Delegation, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("base64-decoding proof: %w", err)
	}
	ct, err := container.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding proof container: %w", err)
	}
	return orderProofChain(ct.Delegations())
}

// orderProofChain returns dlgs reordered root → leaf so that for each
// adjacent pair (a, b), a.Audience() == b.Issuer(). The root is the
// delegation whose issuer is not the audience of any other delegation in the
// set; from there we walk forward following audience → issuer until the set
// is exhausted. Errors if the chain is disconnected, branched, or cyclic.
func orderProofChain(dlgs []ucan.Delegation) ([]ucan.Delegation, error) {
	if len(dlgs) <= 1 {
		return dlgs, nil
	}

	byIssuer := make(map[did.DID]ucan.Delegation, len(dlgs))
	audiences := make(map[did.DID]struct{}, len(dlgs))
	for _, d := range dlgs {
		if _, dup := byIssuer[d.Issuer()]; dup {
			return nil, fmt.Errorf("proof chain has two delegations with the same issuer %s (branched chain)", d.Issuer())
		}
		byIssuer[d.Issuer()] = d
		audiences[d.Audience()] = struct{}{}
	}

	var root ucan.Delegation
	for _, d := range dlgs {
		if _, isAudience := audiences[d.Issuer()]; isAudience {
			continue
		}
		if root != nil {
			return nil, fmt.Errorf("proof chain has multiple roots (issuers %s and %s have no incoming edge)", root.Issuer(), d.Issuer())
		}
		root = d
	}
	if root == nil {
		return nil, fmt.Errorf("proof chain has no root (cycle)")
	}

	ordered := make([]ucan.Delegation, 0, len(dlgs))
	cur := root
	for cur != nil {
		ordered = append(ordered, cur)
		next, ok := byIssuer[cur.Audience()]
		if !ok {
			break
		}
		cur = next
	}

	if len(ordered) != len(dlgs) {
		return nil, fmt.Errorf("proof chain is disconnected: %d delegations supplied but only %d form a contiguous chain", len(dlgs), len(ordered))
	}
	return ordered, nil
}

type ServicesConfig struct {
	Indexer       IndexingServiceConfig      `mapstructure:"indexer" toml:"indexer,omitempty"`
	EgressTracker EgressTrackerServiceConfig `mapstructure:"etracker" toml:"etracker,omitempty"`
	Upload        UploadServiceConfig        `mapstructure:"upload" validate:"required" toml:"upload,omitempty"`
	Publisher     PublisherServiceConfig     `mapstructure:"publisher" toml:"publisher,omitempty"`
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

// IndexingServiceConfig configures the indexing service integration. Like the
// egress tracker, the integration is optional: leaving DID and URL empty
// disables it (claims are neither cached with an indexer nor announced).
type IndexingServiceConfig struct {
	DID   string `mapstructure:"did" flag:"indexing-service-did" toml:"did,omitempty"`
	URL   string `mapstructure:"url" validate:"omitempty,url" flag:"indexing-service-url" toml:"url,omitempty"`
	Proof string `mapstructure:"proof" flag:"indexing-service-proof" toml:"proof,omitempty"`
}

func (s *IndexingServiceConfig) Validate() error {
	return validateConfig(s)
}

func (s *IndexingServiceConfig) ToAppConfig() (app.IndexingServiceConfig, error) {
	if s.DID == "" {
		log.Warn("no indexing service DID provided, indexing is disabled")
		return app.IndexingServiceConfig{}, nil
	}

	if s.URL == "" {
		log.Warn("no indexing service URL provided, indexing is disabled")
		return app.IndexingServiceConfig{}, nil
	}

	sdid, err := did.Parse(s.DID)
	if err != nil {
		return app.IndexingServiceConfig{}, fmt.Errorf("parsing indexing service DID: %w", err)
	}
	surl, err := url.Parse(s.URL)
	if err != nil {
		return app.IndexingServiceConfig{}, fmt.Errorf("parsing indexing service URL: %w", err)
	}
	c, err := client.NewHTTP(surl)
	if err != nil {
		return app.IndexingServiceConfig{}, fmt.Errorf("creating indexing service connection: %w", err)
	}
	out := app.IndexingServiceConfig{
		DID:    sdid,
		Client: c,
	}
	// Parse indexing service proof chain if provided
	if s.Proof != "" {
		chain, err := decodeProofChain(s.Proof)
		if err != nil {
			return app.IndexingServiceConfig{}, fmt.Errorf("parsing indexing service proof: %w", err)
		}
		if len(chain) == 0 {
			return app.IndexingServiceConfig{}, fmt.Errorf("indexing service proof container is empty")
		}
		out.Proofs = chain
	} else {
		// TODO(forrest): in the event a node is run without an indexing service proof, it will
		// almost always fail to index...obviously.
		// The TODO here is one of:
		//   1. Fail to start the node (will be annoying for testing
		//   2. Return an app config with a nil indexing service connection
		//      dependencies of this config are usually fine with a nil connection, as they check it before use.
		log.Warn("no indexing service proof provided, indexing will likely fail, please provide indexing proof")
	}
	return out, nil
}

type EgressTrackerServiceConfig struct {
	DID              string `mapstructure:"did" flag:"egress-tracker-service-did" toml:"did,omitempty"`
	URL              string `mapstructure:"url" flag:"egress-tracker-service-url" toml:"url,omitempty"`
	ReceiptsEndpoint string `mapstructure:"receipts_endpoint" flag:"egress-tracker-service-receipts-endpoint" toml:"receipts_endpoint,omitempty"`
	// According to the spec, batch size should be between 10MiB and 1GiB
	// (see https://github.com/storacha/specs/blob/main/w3-egress-tracking.md)
	MaxBatchSizeBytes int64  `mapstructure:"max_batch_size_bytes" validate:"omitempty,min=10485760,max=1073741824" flag:"egress-tracker-service-max-batch-size-bytes" toml:"max_batch_size_bytes,omitempty"`
	Proof             string `mapstructure:"proof" flag:"egress-tracker-service-proof" toml:"proof,omitempty"`
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

	sdid, err := did.Parse(c.DID)
	if err != nil {
		return app.EgressTrackerServiceConfig{}, fmt.Errorf("parsing egress tracker service DID: %w", err)
	}

	surl, err := url.Parse(c.URL)
	if err != nil {
		return app.EgressTrackerServiceConfig{}, fmt.Errorf("parsing egress tracker service URL: %w", err)
	}

	clnt, err := client.NewHTTP(surl)
	if err != nil {
		return app.EgressTrackerServiceConfig{}, fmt.Errorf("creating egress tracker service connection: %w", err)
	}

	receiptsEndpoint, err := url.Parse(c.ReceiptsEndpoint)
	if err != nil {
		return app.EgressTrackerServiceConfig{}, fmt.Errorf("parsing egress tracker service receipts endpoint: %w", err)
	}

	out := app.EgressTrackerServiceConfig{
		DID:                  sdid,
		Client:               clnt,
		ReceiptsEndpoint:     receiptsEndpoint,
		MaxBatchSizeBytes:    c.MaxBatchSizeBytes,
		CleanupCheckInterval: 1 * time.Hour,
	}

	// Parse egress tracker service proof chain if provided
	if c.Proof != "" {
		chain, err := decodeProofChain(c.Proof)
		if err != nil {
			return app.EgressTrackerServiceConfig{}, fmt.Errorf("parsing egress tracker service proof: %w", err)
		}
		if len(chain) == 0 {
			return app.EgressTrackerServiceConfig{}, fmt.Errorf("egress tracker service proof container is empty")
		}
		out.Proofs = chain
	} else {
		log.Warn("no egress tracker service proof provided, egress tracking is disabled")
	}

	return out, nil
}

type UploadServiceConfig struct {
	DID string `mapstructure:"did" validate:"required" flag:"upload-service-did" toml:"did,omitempty"`
	URL string `mapstructure:"url" validate:"required,url" flag:"upload-service-url" toml:"url,omitempty"`
}

func (s *UploadServiceConfig) Validate() error {
	return validateConfig(s)
}

func (s *UploadServiceConfig) ToAppConfig() (app.UploadServiceConfig, error) {
	sdid, err := did.Parse(s.DID)
	if err != nil {
		return app.UploadServiceConfig{}, fmt.Errorf("parsing upload service DID: %w", err)
	}
	surl, err := url.Parse(s.URL)
	if err != nil {
		return app.UploadServiceConfig{}, fmt.Errorf("parsing upload service URL: %w", err)
	}
	clnt, err := client.NewHTTP(surl)
	if err != nil {
		return app.UploadServiceConfig{}, fmt.Errorf("creating upload service connection: %w", err)
	}
	return app.UploadServiceConfig{
		DID:    sdid,
		Client: clnt,
	}, nil
}

type PublisherServiceConfig struct {
	// AnnounceURLs may be empty: adverts are still built and served locally,
	// but no IPNI node is notified of them.
	AnnounceURLs []string `mapstructure:"ipni_announce_urls" validate:"omitempty,dive,url" flag:"ipni-announce-urls" toml:"ipni_announce_urls,omitempty"`
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
