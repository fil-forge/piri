package publisher

import (
	"net/url"

	ipnipub "github.com/fil-forge/go-libstoracha/ipnipublisher/publisher"
	"github.com/fil-forge/go-ucanto/core/delegation"
	"github.com/multiformats/go-multiaddr"

	"github.com/fil-forge/piri/pkg/config/app"
)

type options struct {
	asyncPublisher        ipnipub.AsyncPublisher
	blobAddr              multiaddr.Multiaddr
	announceAddr          multiaddr.Multiaddr
	announceURLs          []url.URL
	indexingService       app.ServiceConnection
	indexingServiceProofs delegation.Proofs
}

type Option func(*options) error

// WithAsyncPublisher configures the async publisher for IPNI advertisements (overrides any publisher specific config)
func WithAsyncPublisher(p ipnipub.AsyncPublisher) Option {
	return func(o *options) error {
		o.asyncPublisher = p
		return nil
	}
}

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
func WithIndexingService(conn app.ServiceConnection) Option {
	return func(opts *options) error {
		opts.indexingService = conn
		return nil
	}
}
