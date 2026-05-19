package publisher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
	"sync"

	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	ipnimeta "github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/go-ipni-tools/pkg/advertisement"
	"github.com/fil-forge/go-ipni-tools/pkg/metadata"
	ipnipub "github.com/fil-forge/go-ipni-tools/pkg/publisher"
	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/capabilities/assert"
	"github.com/fil-forge/libforge/capabilities/claim"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/ucan/invocation"

	"github.com/fil-forge/piri/lib"
	"github.com/fil-forge/piri/pkg/config/app"
)

// TODO(forrest)[ucan1]: thread safety should be an attribute of the publisher package. Not this ad-hoc shit.
type threadSafeAsyncPublisher struct {
	ipnipub.AsyncPublisher
	mu sync.Mutex
}

func (p *threadSafeAsyncPublisher) Publish(ctx context.Context, pi peer.AddrInfo, contextID string, digests iter.Seq[multihash.Multihash], meta ipnimeta.Metadata) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.AsyncPublisher.Publish(ctx, pi, contextID, digests, meta)
}

var log = logging.Logger("publisher")

type PublisherService struct {
	id                    principal.Signer
	asyncPublisher        ipnipub.AsyncPublisher
	provider              peer.AddrInfo
	indexingService       app.IndexingServiceConfig
	indexingServiceProofs ucan.Delegation
}

func (pub *PublisherService) Publish(ctx context.Context, claim ucan.Delegation) error {
	ability := claim.Command()
	switch ability {
	case assert.LocationCommand:
		err := PublishLocationCommitment(ctx, pub.asyncPublisher, pub.provider, claim)
		if err != nil {
			return err
		}
		return CacheClaim(ctx, pub.id, pub.indexingService, pub.indexingServiceProofs, claim, pub.provider.Addrs)
	default:
		return fmt.Errorf("unknown claim: %s", ability)
	}
}

func PublishLocationCommitment(
	ctx context.Context,
	asyncPublisher ipnipub.AsyncPublisher,
	provider peer.AddrInfo,
	locationCommitment ucan.Delegation,
) error {
	log := log.With("claim", locationCommitment.Link())

	var lc assert.LocationArguments
	if err := lc.UnmarshalCBOR(bytes.NewReader(locationCommitment.Bytes())); err != nil {
		return fmt.Errorf("unmarshalling location commitment: %w", err)
	}

	shardCid, err := advertisement.ShardCID(provider, lc)
	if err != nil {
		return fmt.Errorf(
			"failed to extract shard CID for provider: %s locationCommitment %s: %w",
			provider,
			assert.LocationCommand,
			err,
		)
	}

	var expiration ucan.UnixTimestamp
	if locationCommitment.Expiration() != nil {
		expiration = *locationCommitment.Expiration()
	}

	meta := metadata.MetadataContext.New(
		&metadata.LocationCommitmentMetadata{
			Shard:      shardCid,
			Claim:      locationCommitment.Link(),
			Expiration: int64(expiration),
		},
	)

	contextid, err := advertisement.EncodeContextID(lc.Space, lc.Content)
	if err != nil {
		return fmt.Errorf("encoding advertisement context ID: %w", err)
	}

	err = asyncPublisher.Publish(
		ctx,
		provider,
		string(contextid),
		slices.Values([]multihash.Multihash{lc.Content}),
		meta,
	)
	if err != nil {
		if errors.Is(err, ipnipub.ErrAlreadyAdvertised) {
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
	indexingService app.IndexingServiceConfig,
	invocationProofs ucan.Delegation,
	clm ucan.Delegation,
	providerAddresses []multiaddr.Multiaddr,
) error {
	log := log.With("claim", clm.Link())

	if !indexingService.DID.Defined() {
		log.Warnf("Cannot cache claim - indexing service is not configured")
		return nil
	}

	// TODO I assume claim.Provider.Addresses is a slice of multiaddr byte slices?
	providers := make([][]byte, len(providerAddresses))
	for i, p := range providerAddresses {
		providers[i] = p.Bytes()
	}

	inv, err := claim.Cache.Invoke(
		id,
		indexingService.DID,
		&claim.CacheArguments{
			Claim:    clm.Link(),
			Provider: claim.Provider{Addresses: providers},
		},
		// TODO(forres)[ucan1]: where do we attach the "Proof" for this now?
		// this seems wrong, how does the Cid get to the service?
		invocation.WithProofs(invocationProofs.Link()),
	)
	if err != nil {
		return fmt.Errorf("creating invocation: %w", err)
	}

	// TODO(forrest)[ucan1]: do we need to attach more things to the request?

	res, err := indexingService.Client.Execute(execution.NewRequest(ctx, inv,
		// TODO(forrest)[ucan1]: WithProofs and WithDelegations do the _exact same thing_, pick one kill the other.
		execution.WithProofs(invocationProofs),
		execution.WithDelegations(invocationProofs),
	))
	if err != nil {
		return fmt.Errorf("executing invocation: %w", err)
	}

	if res.Receipt().Out().IsOK() {
		return nil
	}
	// else we be gettin errors
	return fmt.Errorf("failed or some shit idk")

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
	priv, err := crypto.UnmarshalEd25519PrivateKey(id.Raw())
	if err != nil {
		return nil, fmt.Errorf("unmarshaling private key: %w", err)
	}

	asyncPublisher := o.asyncPublisher
	if asyncPublisher == nil {

		announceAddr := o.announceAddr
		if announceAddr == nil {
			announceAddr = publicAddr
		}

		ipnipubOpts := []ipnipub.Option{ipnipub.WithAnnounceAddrs(announceAddr.String())}
		for _, u := range o.announceURLs {
			log.Infof("Announcing new IPNI adverts to: %s", u.String())
			ipnipubOpts = append(ipnipubOpts, ipnipub.WithDirectAnnounce(u.String()))
		}
		ipniPublisher, err := ipnipub.New(priv, publisherStore, ipnipubOpts...)
		if err != nil {
			return nil, fmt.Errorf("creating IPNI publisher instance: %w", err)
		}

		asyncPublisher = &threadSafeAsyncPublisher{AsyncPublisher: ipnipub.AsyncFrom(ipniPublisher)}
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

	if !o.indexingService.DID.Defined() {
		log.Errorf("Indexing service is not configured - claims will not be cached")
	}

	return &PublisherService{
		id:                    id,
		asyncPublisher:        asyncPublisher,
		provider:              provInfo,
		indexingService:       o.indexingService,
		indexingServiceProofs: o.indexingServiceProofs,
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

func asCID(link ipld.Link) cid.Cid {
	if cl, ok := link.(cidlink.Link); ok {
		return cl.Cid
	}
	return cid.MustParse(link.String())
}
