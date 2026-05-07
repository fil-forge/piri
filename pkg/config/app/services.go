package app

import (
	"net/url"
	"time"

	"github.com/fil-forge/go-ucanto/client"
	"github.com/fil-forge/go-ucanto/core/delegation"
	"github.com/multiformats/go-multiaddr"
)

type ExternalServicesConfig struct {
	PrincipalMapping map[string]string

	Indexer       IndexingServiceConfig
	EgressTracker EgressTrackerServiceConfig
	Upload        UploadServiceConfig
	Publisher     PublisherServiceConfig
}

// IndexingServiceConfig contains indexing service connection and proof(s) for
// using the service
type IndexingServiceConfig struct {
	Connection client.Connection
	Proofs     delegation.Proofs
}

type EgressTrackerServiceConfig struct {
	Connection           client.Connection
	Proofs               delegation.Proofs
	ReceiptsEndpoint     *url.URL
	MaxBatchSizeBytes    int64
	CleanupCheckInterval time.Duration
}

type UploadServiceConfig struct {
	Connection client.Connection
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
