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

	errdm "github.com/fil-forge/ucantone/errors/datamodel"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/multikey"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	ipnimeta "github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/go-ipni-tools/pkg/advertisement"
	"github.com/fil-forge/go-ipni-tools/pkg/metadata"
	ipnipub "github.com/fil-forge/go-ipni-tools/pkg/publisher"
	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/commands/assert"
	"github.com/fil-forge/libforge/commands/claim"
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
	id                    ucan.Issuer
	asyncPublisher        ipnipub.AsyncPublisher
	provider              peer.AddrInfo
	indexingService       app.IndexingServiceConfig
	indexingServiceProofs []ucan.Delegation
}

func (pub *PublisherService) Publish(ctx context.Context, claim ucan.Invocation) error {
	ability := claim.Command()
	switch ability {
	case assert.Location.Command:
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
	locationCommitment ucan.Invocation,
) error {
	log := log.With("claim", locationCommitment.Link())

	// ArgumentsBytes returns the raw CBOR map for the invocation args;
	// Bytes() returns the whole signed envelope, which can't be decoded
	// as LocationArguments directly.
	var lc assert.LocationArguments
	if err := lc.UnmarshalCBOR(bytes.NewReader(locationCommitment.ArgumentsBytes())); err != nil {
		return fmt.Errorf("unmarshalling location commitment: %w", err)
	}

	shardCid, err := advertisement.ShardCID(provider, lc)
	if err != nil {
		return fmt.Errorf(
			"failed to extract shard CID for provider: %s locationCommitment %s: %w",
			provider,
			assert.Location.Command,
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
	id ucan.Issuer,
	indexingService app.IndexingServiceConfig,
	invocationProofs []ucan.Delegation,
	clm ucan.Invocation,
	providerAddresses []multiaddr.Multiaddr,
) error {
	log := log.With("claim", clm.Link())

	if !indexingService.DID.Defined() {
		log.Warnf("Cannot cache claim - indexing service is not configured")
		return nil
	}
	if len(invocationProofs) == 0 {
		return fmt.Errorf("no proofs configured for indexing service invocation")
	}

	providers := make([][]byte, len(providerAddresses))
	for i, p := range providerAddresses {
		providers[i] = p.Bytes()
	}

	// The proof chain runs root → leaf. The invocation's WithProofs links must
	// reference the leaf delegation (which directly authorizes this operator);
	// every other link in the chain (e.g. indexing-service → delegator) is
	// supplied as a supporting delegation so the validator can walk it back.
	proofLinks := make([]cid.Cid, len(invocationProofs))
	for i, d := range invocationProofs {
		proofLinks[i] = d.Link()
	}

	inv, err := claim.Cache.Invoke(
		id,
		indexingService.DID,
		&claim.CacheArguments{
			Claim:    clm.Link(),
			Provider: claim.Provider{Addresses: providers},
		},
		invocation.WithProofs(proofLinks...),
	)
	if err != nil {
		return fmt.Errorf("creating invocation: %w", err)
	}

	res, err := indexingService.Client.Execute(execution.NewRequest(ctx, inv,
		execution.WithDelegations(invocationProofs...),
		execution.WithInvocations(clm),
	))
	if err != nil {
		return fmt.Errorf("executing invocation: %w", err)
	}

	if res.Receipt().Out().IsOK() {
		return nil
	}

	_, errBytes := res.Receipt().Out().Unpack()
	var rcptErr errdm.ErrorModel
	if err := rcptErr.UnmarshalCBOR(bytes.NewReader(errBytes)); err != nil {
		return fmt.Errorf("unmarshaling receipt error: %w", err)
	}
	return rcptErr
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
	id multikey.Issuer,
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
	// ucantone's ed25519 Signer.Raw() returns the 32-byte seed; libp2p
	// expects the 64-byte Go private-key form (seed || pub). Expand here.
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
