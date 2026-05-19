package app

import (
	"net/url"
	"time"

	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/multiformats/go-multiaddr"
)

type ExternalServicesConfig struct {
	Indexer       IndexingServiceConfig
	EgressTracker EgressTrackerServiceConfig
	Upload        UploadServiceConfig
	Publisher     PublisherServiceConfig
}

// IndexingServiceConfig contains indexing service connection and proof(s) for
// using the service
type IndexingServiceConfig struct {
	DID    did.DID
	Client execution.Executor
	Proofs ucan.Delegation
}

type EgressTrackerServiceConfig struct {
	DID                  did.DID
	Client               execution.Executor
	Proofs               ucan.Delegation
	ReceiptsEndpoint     *url.URL
	MaxBatchSizeBytes    int64
	CleanupCheckInterval time.Duration
}

type UploadServiceConfig struct {
	DID    did.DID
	Client execution.Executor
}

type PublisherServiceConfig struct {
	// The public facing multiaddr of the publisher
	PublicMaddr multiaddr.Multiaddr
	// The address put into announce messages to tell indexers where to fetch advertisements from
	AnnounceMaddr multiaddr.Multiaddr
	// Address to tell indexers where to fetch blobs from
	BlobMaddr multiaddr.Multiaddr
	// Indexer URLs to send direct HTTP announcements to
	AnnounceURLs []url.URL
}
