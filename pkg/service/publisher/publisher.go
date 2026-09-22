package publisher

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"slices"
	"sync"

	errdm "github.com/fil-forge/ucantone/errors/datamodel"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/multikey"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
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
	"github.com/fil-forge/piri/pkg/service/publisher/advert"
)

var log = logging.Logger("publisher")

type PublisherService struct {
	id ucan.Issuer
	// mu serialises everything that decides what the node advertises: the
	// IPNI publisher chains each advertisement to the previous head and is
	// not safe for concurrent use, and a withdrawal must not slip between a
	// batch deciding its contents and committing them.
	mu                    sync.Mutex
	ipni                  ipnipub.BatchPublisher
	queue                 AdvertQueue
	provider              peer.AddrInfo
	indexingService       app.IndexingServiceConfig
	indexingServiceProofs []ucan.Delegation
}

// Publish records that the claim's IPNI advertisement is owed, together with
// what to advertise, and caches the claim with the indexing service. The
// advertisement itself is published later by the IPNIPublish task, in a batch
// with its neighbours, so an accept returns before it exists; the indexer is
// told now, since that is what serves a read of the blob straight after its
// write.
func (pub *PublisherService) Publish(ctx context.Context, claim ucan.Invocation) error {
	ability := claim.Command()
	switch ability {
	case assert.Location.Command:
		spec, err := locationAdvertSpec(pub.provider, claim)
		if err != nil {
			return fmt.Errorf("deriving advertisement for claim %s: %w", claim.Link(), err)
		}
		if err := pub.queue.Enqueue(ctx, claim.Link(), spec); err != nil {
			return err
		}
		return CacheClaim(ctx, pub.id, pub.indexingService, pub.indexingServiceProofs, claim, pub.provider.Addrs)
	default:
		return fmt.Errorf("unknown claim: %s", ability)
	}
}

// Withdrawer takes a claim's advertisement out of the queue under the
// publishing lock, so it waits for any batch mid-publish: the withdrawal
// either precedes a batch loading its rows, which then no longer include the
// claim, or follows the commit. In neither case is a location for a released
// blob published after the release has returned.
type Withdrawer interface {
	Withdraw(ctx context.Context, claim cid.Cid) error
}

// Withdraw implements Withdrawer.
func (pub *PublisherService) Withdraw(ctx context.Context, claim cid.Cid) error {
	pub.mu.Lock()
	defer pub.mu.Unlock()
	return pub.queue.Dequeue(ctx, claim)
}

// PublishClaimed publishes the advertisements of the rows a task has claimed,
// under one commit, and reports how many rows the batch held and how many of
// them were published. It holds the publishing lock from loading the rows to
// committing, so a Withdraw either precedes the load or waits for the commit.
//
// A row without a spec was queued before the spec was stored with it; it is
// skipped with a warning and retired with the batch. Nothing consumes the
// advertisement chain yet, so the gap is harmless for now; once such rows
// have drained, skipping should become a failure.
func (pub *PublisherService) PublishClaimed(ctx context.Context, batch harmonytask.TaskID) (rows, published int, err error) {
	pub.mu.Lock()
	defer pub.mu.Unlock()

	queued, err := pub.queue.Claimed(ctx, batch)
	if err != nil {
		return 0, 0, fmt.Errorf("loading batch: %w", err)
	}
	specs := make([]ipnipub.AdvertSpec, 0, len(queued))
	for _, row := range queued {
		if row.Spec == nil {
			log.Warnw("skipping queued advertisement without a usable spec", "claim", row.Claim)
			continue
		}
		meta := metadata.MetadataContext.New()
		if err := meta.UnmarshalBinary(row.Spec.Metadata); err != nil {
			log.Errorw("skipping queued advertisement with undecodable metadata", "claim", row.Claim, "error", err)
			continue
		}
		specs = append(specs, ipnipub.AdvertSpec{
			ContextID: string(row.Spec.ContextID),
			Digests:   slices.Values([]multihash.Multihash{row.Spec.Digest}),
			Metadata:  meta,
		})
	}
	if len(specs) == 0 {
		return len(queued), 0, nil
	}
	if _, err := pub.ipni.PublishBatch(ctx, pub.provider, specs); err != nil {
		return len(queued), 0, fmt.Errorf("publishing %d advertisements: %w", len(specs), err)
	}
	return len(queued), len(specs), nil
}

// locationAdvertSpec is what a location commitment advertises: its content
// under the context ID derived from the space and content, with metadata
// naming the claim and its shard. The shard is derived from the provider's
// addresses as they are now; the advertisement's own addresses are taken
// from the provider again when it is published.
func locationAdvertSpec(provider peer.AddrInfo, locationCommitment ucan.Invocation) (advert.Spec, error) {
	if locationCommitment.Command() != assert.Location.Command {
		return advert.Spec{}, fmt.Errorf("not a location commitment: %s", locationCommitment.Command())
	}
	// ArgumentsBytes returns the raw CBOR map for the invocation args;
	// Bytes() returns the whole signed envelope, which can't be decoded
	// as LocationArguments directly.
	var lc assert.LocationArguments
	if err := lc.UnmarshalCBOR(bytes.NewReader(locationCommitment.ArgumentsBytes())); err != nil {
		return advert.Spec{}, fmt.Errorf("unmarshalling location commitment: %w", err)
	}

	shardCid, err := advertisement.ShardCID(provider, lc)
	if err != nil {
		return advert.Spec{}, fmt.Errorf(
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

	md := metadata.MetadataContext.New(
		&metadata.LocationCommitmentMetadata{
			Shard:      shardCid,
			Claim:      locationCommitment.Link(),
			Expiration: int64(expiration),
		},
	)
	meta, err := md.MarshalBinary()
	if err != nil {
		return advert.Spec{}, fmt.Errorf("marshalling advertisement metadata: %w", err)
	}

	contextid, err := advertisement.EncodeContextID(lc.Space, lc.Content)
	if err != nil {
		return advert.Spec{}, fmt.Errorf("encoding advertisement context ID: %w", err)
	}

	return advert.Spec{ContextID: contextid, Digest: lc.Content, Metadata: meta}, nil
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
	queue AdvertQueue,
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
		ipni:                  ipniPublisher,
		queue:                 queue,
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
