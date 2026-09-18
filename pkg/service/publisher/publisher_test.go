package publisher

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/fil-forge/go-ipni-tools/pkg/advertisement"
	"github.com/fil-forge/go-ipni-tools/pkg/metadata"
	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/commands"
	"github.com/fil-forge/libforge/commands/assert"
	"github.com/fil-forge/libforge/commands/claim"
	"github.com/fil-forge/libforge/digestutil"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/binding"
	"github.com/fil-forge/ucantone/client"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
)

// memQueue is an in-memory AdvertQueue that records what is owed.
type memQueue struct {
	mu   sync.Mutex
	owed []cid.Cid
}

func (q *memQueue) Enqueue(_ context.Context, claim cid.Cid) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.owed = append(q.owed, claim)
	return nil
}

func (q *memQueue) Dequeue(_ context.Context, claim cid.Cid) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.owed = slices.DeleteFunc(q.owed, func(c cid.Cid) bool { return c == claim })
	return nil
}

func (q *memQueue) links() []cid.Cid {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]cid.Cid(nil), q.owed...)
}

// newTestService builds a service over fresh in-memory stores and returns the
// queue and claim store beside it, since Accept writes the claim to the store
// before publishing and the task reads it back from there.
func newTestService(t *testing.T, publisherStore store.PublisherStore, addr multiaddr.Multiaddr, opts ...Option) (*PublisherService, *memQueue, invocationstore.InvocationStore) {
	t.Helper()
	queue := &memQueue{}
	claims := invocationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
	svc, err := New(testutil.Alice, publisherStore, addr, queue, claims, opts...)
	require.NoError(t, err)
	return svc, queue, claims
}

func TestPublisherService(t *testing.T) {
	addr, err := multiaddr.NewMultiaddr("/dns4/localhost/tcp/3000/http")
	require.NoError(t, err)

	ctx := t.Context()

	t.Run("queues a location commitment and publishes it in a batch", func(t *testing.T) {
		dstore := dssync.MutexWrap(datastore.NewMapDatastore())
		publisherStore := store.FromDatastore(dstore, store.WithMetadataContext(metadata.MetadataContext))
		svc, queue, claims := newTestService(t, publisherStore, addr)

		space := testutil.RandomDID(t)
		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)

		claimInv := mintLocationClaim(t, space, shard, *location)
		require.NoError(t, claims.Put(ctx, claimInv))

		// Publish owes the advertisement rather than writing it.
		require.NoError(t, svc.Publish(ctx, claimInv))
		require.Equal(t, []cid.Cid{claimInv.Link()}, queue.links())
		_, err := publisherStore.Head(ctx)
		require.True(t, store.IsNotFound(err), "nothing is published on the request path")

		// The task publishes what is owed.
		require.NoError(t, svc.PublishClaims(ctx, queue.links()))

		hd, err := publisherStore.Head(ctx)
		require.NoError(t, err)

		ad, err := publisherStore.Advert(ctx, hd.Head)
		require.NoError(t, err)

		expectCtxID := testutil.Must(advertisement.EncodeContextID(space, shard))(t)
		require.Equal(t, expectCtxID, ad.ContextID)

		meta := metadata.MetadataContext.New()
		require.NoError(t, meta.UnmarshalBinary(ad.Metadata))

		protocol := meta.Get(metadata.LocationCommitmentID)
		require.NotNil(t, protocol)
		lcmeta, ok := protocol.(*metadata.LocationCommitmentMetadata)
		require.True(t, ok)
		require.Equal(t, claimInv.Link().String(), lcmeta.Claim.String())

		var ents []multihash.Multihash
		for digest, err := range publisherStore.Entries(ctx, ad.Entries) {
			require.NoError(t, err)
			ents = append(ents, digest)
		}
		require.Len(t, ents, 1)
		require.Equal(t, shard, ents[0])
	})

	t.Run("allow skip publish existing advert", func(t *testing.T) {
		dstore := dssync.MutexWrap(datastore.NewMapDatastore())
		publisherStore := store.FromDatastore(dstore, store.WithMetadataContext(metadata.MetadataContext))
		svc, _, claims := newTestService(t, publisherStore, addr)

		space := testutil.RandomDID(t)
		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
		claimInv := mintLocationClaim(t, space, shard, *location)
		require.NoError(t, claims.Put(ctx, claimInv))

		// The first batch writes the advert; the second finds it already
		// advertised and skips it, so a retried batch is harmless.
		require.NoError(t, svc.PublishClaims(ctx, []cid.Cid{claimInv.Link()}))
		hd, err := publisherStore.Head(ctx)
		require.NoError(t, err)
		require.NoError(t, svc.PublishClaims(ctx, []cid.Cid{claimInv.Link()}))
		again, err := publisherStore.Head(ctx)
		require.NoError(t, err)
		require.Equal(t, hd.Head, again.Head, "an already advertised claim moves nothing")
	})

	t.Run("skips a claim released before it was published", func(t *testing.T) {
		dstore := dssync.MutexWrap(datastore.NewMapDatastore())
		publisherStore := store.FromDatastore(dstore, store.WithMetadataContext(metadata.MetadataContext))
		svc, queue, claims := newTestService(t, publisherStore, addr)

		space := testutil.RandomDID(t)
		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
		claimInv := mintLocationClaim(t, space, shard, *location)
		require.NoError(t, claims.Put(ctx, claimInv))
		require.NoError(t, svc.Publish(ctx, claimInv))

		// Released before the flush: the claim is gone from the store. A
		// task that had already claimed the row still asks for it.
		require.NoError(t, claims.Delete(ctx, claimInv.Link()))
		require.NoError(t, queue.Dequeue(ctx, claimInv.Link()))
		require.NoError(t, svc.PublishClaims(ctx, []cid.Cid{claimInv.Link()}))
		_, err := publisherStore.Head(ctx)
		require.True(t, store.IsNotFound(err), "no location is advertised for a released blob")
	})

	t.Run("a withdrawal waits for the batch in flight", func(t *testing.T) {
		dstore := dssync.MutexWrap(datastore.NewMapDatastore())
		publisherStore := store.FromDatastore(dstore, store.WithMetadataContext(metadata.MetadataContext))
		queue := &memQueue{}
		claims := &claimsObservingLoad{InvocationStore: invocationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))}
		svc, err := New(testutil.Alice, publisherStore, addr, queue, claims)
		require.NoError(t, err)

		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
		clm := mintLocationClaim(t, testutil.RandomDID(t), shard, *location)
		require.NoError(t, claims.Put(ctx, clm))
		require.NoError(t, svc.Publish(ctx, clm))

		// Release arrives while the batch is loading its claims under the
		// lock: it must not get in until the batch has committed.
		withdrawn := make(chan error, 1)
		claims.onGetAll = func(ctx context.Context) {
			go func() { withdrawn <- svc.Withdraw(ctx, clm.Link()) }()
			select {
			case err := <-withdrawn:
				t.Errorf("withdrawal completed while the batch held the lock (err=%v)", err)
				// Put it back so the receive below does not hang the test.
				withdrawn <- err
			case <-time.After(50 * time.Millisecond):
			}
		}
		require.NoError(t, svc.PublishClaims(ctx, []cid.Cid{clm.Link()}))
		require.NoError(t, <-withdrawn, "the withdrawal goes through once the batch has committed")
		require.Empty(t, queue.links())
		_, err = publisherStore.Head(ctx)
		require.NoError(t, err, "the batch that held the lock committed")
	})

	t.Run("caches claims", func(t *testing.T) {
		dstore := dssync.MutexWrap(datastore.NewMapDatastore())
		publisherStore := store.FromDatastore(dstore, store.WithMetadataContext(metadata.MetadataContext))

		// Mock indexing service: an in-process UCAN HTTP server signed
		// by Bob with a single /claim/cache handler. server.NewHTTP is
		// both an http.Handler and an http.RoundTripper, so a client
		// configured to use it as Transport round-trips invocations
		// without binding a real port.
		var (
			handlerCalled bool
			receivedClaim cid.Cid
		)
		srv := server.NewHTTP(testutil.Bob)
		srv.Handle(claim.Cache.Command, binding.NewHandler(
			func(req *binding.Request[*claim.CacheArguments], res *binding.Response[*claim.CacheOK]) error {
				handlerCalled = true
				receivedClaim = req.Task().Arguments().Claim
				return res.SetSuccess(&claim.CacheOK{})
			},
		))

		endpoint, err := url.Parse("http://test")
		require.NoError(t, err)
		httpClient, err := client.NewHTTP(endpoint, client.WithHTTPClient(&http.Client{Transport: srv}))
		require.NoError(t, err)

		// Bob authorises Alice to invoke /claim/cache on Bob.
		proof, err := delegation.Delegate(
			testutil.Bob,
			testutil.Alice.DID(),
			testutil.Bob.DID(),
			ucan.Command(claim.Cache.Command),
		)
		require.NoError(t, err)

		svc, _, _ := newTestService(t, publisherStore, addr,
			WithIndexingService(app.IndexingServiceConfig{
				DID:    testutil.Bob.DID(),
				Client: httpClient,
			}),
			WithIndexingServiceProof([]ucan.Delegation{proof}),
		)

		space := testutil.RandomDID(t)
		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
		claimInv := mintLocationClaim(t, space, shard, *location)

		require.NoError(t, svc.Publish(ctx, claimInv))
		require.True(t, handlerCalled, "indexing-service /claim/cache handler was invoked")
		require.Equal(t, claimInv.Link(), receivedClaim,
			"handler received the location-claim CID as args.Claim")
	})
}

// mintLocationClaim builds a signed /assert/location invocation matching
// the shape the production blob/accept handler produces
// (pkg/ucanhandlers/blob/accept.go:173–183). Signed by Alice — the
// publisher is also Alice in these tests.
func mintLocationClaim(t *testing.T, space did.DID, content multihash.Multihash, location url.URL) ucan.Invocation {
	t.Helper()
	inv, err := assert.Location.Invoke(
		testutil.Alice,
		space,
		&assert.LocationArguments{
			Space:    space,
			Content:  content,
			Location: []commands.CborURL{commands.CborURL(location)},
		},
		invocation.WithNoExpiration(),
	)
	require.NoError(t, err)
	return inv
}

// claimsObservingLoad is a claim store that runs a hook when a batch loads
// its claims, which PublishClaims does under the publishing lock.
type claimsObservingLoad struct {
	invocationstore.InvocationStore
	onGetAll func(context.Context)
}

func (c *claimsObservingLoad) GetAll(ctx context.Context, links []cid.Cid) (map[cid.Cid]ucan.Invocation, error) {
	if c.onGetAll != nil {
		c.onGetAll(ctx)
	}
	return c.InvocationStore.GetAll(ctx, links)
}
