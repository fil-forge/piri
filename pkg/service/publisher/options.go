package publisher

import (
	"net/url"

	ipnipub "github.com/fil-forge/go-ipni-tools/pkg/publisher"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/multiformats/go-multiaddr"

	"github.com/fil-forge/piri/pkg/config/app"
)

type options struct {
	asyncPublisher        ipnipub.AsyncPublisher
	blobAddr              multiaddr.Multiaddr
	announceAddr          multiaddr.Multiaddr
	announceURLs          []url.URL
	indexingService       app.IndexingServiceConfig
	indexingServiceProofs []ucan.Delegation
}

type Option func(*options) error

// WithAnnounceAddress sets the address put into announce messages to tell
// indexers where to fetch advertisements from.
func WithAnnounceAddress(addr multiaddr.Multiaddr) Option {
	return func(o *options) error {
		o.announceAddr = addr
		return nil
	}
}

// WithBlobAddress sets a custom address to tell indexers where to fetch blobs from
func WithBlobAddress(addr multiaddr.Multiaddr) Option {
	return func(o *options) error {
		o.blobAddr = addr
		return nil
	}
}

// WithDirectAnnounce sets indexer URLs to send direct HTTP announcements to.
func WithDirectAnnounce(announceURLs ...url.URL) Option {
	return func(o *options) error {
		o.announceURLs = append(o.announceURLs, announceURLs...)
		return nil
	}
}

// WithIndexingService sets the client connection to the indexing UCAN service.
func WithIndexingService(conn app.IndexingServiceConfig) Option {
	return func(opts *options) error {
		opts.indexingService = conn
		return nil
	}
}

// WithIndexingServiceProof configures the proof chain (root → leaf) for UCAN
// invocations to the indexing service. Every delegation in the chain is
// attached to each /claim/cache invocation; passing only the leaf yields
// "delegation issuer is X not Y" failures inside the indexer's validator
// because the chain back to the original authority is broken.
func WithIndexingServiceProof(proofs []ucan.Delegation) Option {
	return func(opts *options) error {
		opts.indexingServiceProofs = proofs
		return nil
	}
}
