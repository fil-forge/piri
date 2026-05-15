package app

import (
	"net/url"
	"time"

	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/execution"
	"github.com/multiformats/go-multiaddr"
)

// ServiceConnection bundles the DID of an upstream service with the
// ucantone HTTP client used to invoke capabilities on it.
//
// This is the canonical "connection" shape that the storage, retrieval,
// signer, egress-tracker and principal resolver code paths all consume.
type ServiceConnection struct {
	DID    did.DID
	Client execution.Executor
}

type ExternalServicesConfig struct {
	Indexer       IndexingServiceConfig
	EgressTracker EgressTrackerServiceConfig
	Upload        UploadServiceConfig
	Publisher     PublisherServiceConfig
}

// IndexingServiceConfig contains indexing service connection and proof(s) for
// using the service
type IndexingServiceConfig struct {
	Connection ServiceConnection
}

type EgressTrackerServiceConfig struct {
	Connection           ServiceConnection
	ReceiptsEndpoint     *url.URL
	MaxBatchSizeBytes    int64
	CleanupCheckInterval time.Duration
}

type UploadServiceConfig struct {
	Connection ServiceConnection
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
