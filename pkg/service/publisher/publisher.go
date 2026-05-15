package publisher

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"iter"
	"slices"
	"sync"

	logging "github.com/ipfs/go-log/v2"
	ipnimeta "github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/go-libstoracha/metadata"

	"github.com/fil-forge/go-ipni-tools/pkg/advertisement"
	"github.com/fil-forge/go-ipni-tools/pkg/publisher"
	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/capabilities/assert"
	"github.com/fil-forge/libforge/capabilities/claim"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/ucan/invocation"

	"github.com/fil-forge/piri/lib"
	"github.com/fil-forge/piri/pkg/config/app"
)

type threadSafeAsyncPublisher struct {
	publisher.AsyncPublisher
	mu sync.Mutex
}

func (p *threadSafeAsyncPublisher) Publish(
	ctx context.Context,
	pi peer.AddrInfo,
	contextID string,
	digests iter.Seq[multihash.Multihash],
	meta ipnimeta.Metadata,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.AsyncPublisher.Publish(ctx, pi, contextID, digests, meta)
}

var log = logging.Logger("publisher")

type PublisherService struct {
	id              principal.Signer
	store           store.PublisherStore
	asyncPublisher  publisher.AsyncPublisher
	provider        peer.AddrInfo
	indexingService app.ServiceConnection
}

func (pub *PublisherService) Store() store.PublisherStore {
	return pub.store
}

func (pub *PublisherService) Publish(ctx context.Context, claim *invocation.Invocation) error {
	if claim.Command() != assert.LocationCommand {
		return fmt.Errorf("unknown claim command: %q, expected %q", claim.Command(), assert.LocationCommand)
	}
	if err := PublishLocationCommitment(ctx, pub.asyncPublisher, pub.provider, claim); err != nil {
		return err
	}
	return CacheClaim(ctx, pub.id, pub.indexingService, claim, pub.provider.Addrs)
}

func PublishLocationCommitment(
	ctx context.Context,
	asyncPublisher publisher.AsyncPublisher,
	provider peer.AddrInfo,
	inv *invocation.Invocation,
) error {
	log := log.With("claim", inv.Link())

	var args assert.LocationArguments
	if err := args.UnmarshalCBOR(bytes.NewReader(inv.ArgumentsBytes())); err != nil {
		return fmt.Errorf("decoding location commitment arguments: %w", err)
	}

	digests := []multihash.Multihash{args.Content}
	contextid, err := advertisement.EncodeContextID(args.Space, args.Content)
	if err != nil {
		return fmt.Errorf("encoding advertisement context ID: %w", err)
	}

	var exp int64
	if inv.Expiration() != nil {
		exp = int64(*inv.Expiration())
	}

	shardCid, err := advertisement.ShardCID(provider, args)
	if err != nil {
		return fmt.Errorf("failed to extract shard CID for provider: %s locationCommitment %s: %w", provider, assert.LocationCommand, err)
	}

	// TODO(forrest)[ucan1]: Likely we will want to migrate this type to libforge to go-ipni-tools
	meta := metadata.MetadataContext.New(
		&metadata.LocationCommitmentMetadata{
			Shard:      shardCid,
			Claim:      inv.Link(),
			Expiration: exp,
		},
	)

	err = asyncPublisher.Publish(ctx, provider, string(contextid), slices.Values(digests), meta)
	if err != nil {
		if errors.Is(err, publisher.ErrAlreadyAdvertised) {
			log.Warnf("Skipping previously published claim")
			return nil
		}
		return fmt.Errorf("publishing claim: %w", err)
	}

	return nil
}

func CacheClaim(
	ctx context.Context,
	id principal.Signer,
	indexingService app.ServiceConnection,
	inv *invocation.Invocation,
	providerAddresses []multiaddr.Multiaddr,
) error {
	log := log.With("claim", inv.Link())

	if indexingService.Client == nil {
		log.Warnf("Cannot cache claim - indexing service is not configured")
		return nil
	}

	var args assert.LocationArguments
	if err := args.UnmarshalCBOR(bytes.NewReader(inv.ArgumentsBytes())); err != nil {
		return fmt.Errorf("decoding location commitment arguments: %w", err)
	}

	// TODO unsure if Provider expects cbor encoded data, or just the bytes, assume the latter
	paddrs := make([][]byte, len(providerAddresses))
	for i, p := range providerAddresses {
		paddrs[i] = p.Bytes()
	}

	cachInv, err := claim.Cache.Invoke(
		id,
		indexingService.DID,
		&claim.CacheArguments{
			Claim: inv.Link(),
			Provider: claim.Provider{
				Addresses: paddrs,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("creating cache claim invocation: %w", err)
	}

	resp, err := indexingService.Client.Execute(execution.NewRequest(ctx, inv, execution.WithInvocations(cachInv)))
	if err != nil {
		return fmt.Errorf("executing invocation: %w", err)
	}

	if resp.Receipt().Out().IsErr() {
		log.Error("failed to cached location commitment with indexing service")
		return fmt.Errorf("failed to cache location claim with indexing service")
	}

	return nil
}

var _ Publisher = (*PublisherService)(nil)

// New creates a [Publisher] that publishes content claims/commitments to IPNI
// and caches them with the indexing service.
//
// The publicAddr parameter is the base public address where adverts and claims
// can be read from. When publishing, the address is suffixed with a
// /http-path/<path> multiaddr, where "path" is the URI encoded version of the
// configured claim path.
//
// Note: publicAddr address must be HTTP(S).
func New(
	id principal.Signer,
	publisherStore store.PublisherStore,
	publicAddr multiaddr.Multiaddr,
	opts ...Option,
) (*PublisherService, error) {
	o := &options{}
	for _, opt := range opts {
		err := opt(o)
		if err != nil {
			return nil, err
		}
	}
	// id.Raw() is the 32-byte ed25519 seed; expand it to the 64-byte private
	// key that libp2p's crypto package expects.
	priv, err := crypto.UnmarshalEd25519PrivateKey(ed25519.NewKeyFromSeed(id.Raw()))
	if err != nil {
		return nil, fmt.Errorf("unmarshaling private key: %w", err)
	}

	asyncPublisher := o.asyncPublisher
	if asyncPublisher == nil {

		announceAddr := o.announceAddr
		if announceAddr == nil {
			announceAddr = publicAddr
		}

		ipnipubOpts := []publisher.Option{publisher.WithAnnounceAddrs(announceAddr.String())}
		for _, u := range o.announceURLs {
			log.Infof("Announcing new IPNI adverts to: %s", u.String())
			ipnipubOpts = append(ipnipubOpts, publisher.WithDirectAnnounce(u.String()))
		}
		ipniPublisher, err := publisher.New(priv, publisherStore, ipnipubOpts...)
		if err != nil {
			return nil, fmt.Errorf("creating IPNI publisher instance: %w", err)
		}

		asyncPublisher = &threadSafeAsyncPublisher{AsyncPublisher: publisher.AsyncFrom(ipniPublisher)}
	}

	found := false
	for _, p := range publicAddr.Protocols() {
		if p.Code == multiaddr.P_HTTPS || p.Code == multiaddr.P_HTTP {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("IPNI publisher address is not HTTP(S): %s", publicAddr)
	}

	peerid, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("creating libp2p peer ID from private key: %w", err)
	}
	provInfo, err := providerInfo(peerid, publicAddr, o.blobAddr)
	if err != nil {
		return nil, fmt.Errorf("building provider info: %w", err)
	}

	if o.indexingService.Client == nil {
		log.Errorf("Indexing service is not configured - claims will not be cached")
	}

	return &PublisherService{
		id:              id,
		store:           publisherStore,
		asyncPublisher:  asyncPublisher,
		provider:        provInfo,
		indexingService: o.indexingService,
	}, nil
}

func providerInfo(peerID peer.ID, publicAddr multiaddr.Multiaddr, blobAddr multiaddr.Multiaddr) (peer.AddrInfo, error) {
	provider := peer.AddrInfo{ID: peerID}
	if blobAddr == nil {
		addr, err := lib.JoinHTTPPath(publicAddr, "blob/{blob}")
		if err != nil {
			return peer.AddrInfo{}, fmt.Errorf("joining blob pattern path to public multiaddr: %w", err)
		}
		blobAddr = addr
	}
	provider.Addrs = append(provider.Addrs, blobAddr)

	claimAddr, err := lib.JoinHTTPPath(publicAddr, "claim/{claim}")
	if err != nil {
		return peer.AddrInfo{}, fmt.Errorf("joining claim pattern path to public multiaddr: %w", err)
	}
	provider.Addrs = append(provider.Addrs, claimAddr)

	return provider, nil
}
