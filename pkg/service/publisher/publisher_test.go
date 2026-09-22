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
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/service/publisher/advert"
)

// memQueue is an in-memory AdvertQueue that records what is owed. It hands
// every row to any batch that asks, and runs onClaimed, if set, while doing
// so, which PublishClaimed does under the publishing lock.
type memQueue struct {
	mu        sync.Mutex
	rows      []QueuedAdvert
	onClaimed func(context.Context)
}

func (q *memQueue) Enqueue(_ context.Context, claim cid.Cid, spec advert.Spec) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.rows = append(q.rows, QueuedAdvert{Claim: claim, Spec: &spec})
	return nil
}

func (q *memQueue) Dequeue(_ context.Context, claim cid.Cid) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.rows = slices.DeleteFunc(q.rows, func(r QueuedAdvert) bool { return r.Claim == claim })
	return nil
}

func (q *memQueue) Claimed(ctx context.Context, _ harmonytask.TaskID) ([]QueuedAdvert, error) {
	if q.onClaimed != nil {
		q.onClaimed(ctx)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]QueuedAdvert(nil), q.rows...), nil
}

func (q *memQueue) links() []cid.Cid {
	q.mu.Lock()
	defer q.mu.Unlock()
	links := make([]cid.Cid, len(q.rows))
	for i, r := range q.rows {
		links[i] = r.Claim
	}
	return links
}

// newTestService builds a service over a fresh in-memory queue and returns
// the queue beside it.
func newTestService(t *testing.T, publisherStore store.PublisherStore, addr multiaddr.Multiaddr, opts ...Option) (*PublisherService, *memQueue) {
	t.Helper()
	queue := &memQueue{}
	svc, err := New(testutil.Alice, publisherStore, addr, queue, opts...)
	require.NoError(t, err)
	return svc, queue
}

func TestPublisherService(t *testing.T) {
	addr, err := multiaddr.NewMultiaddr("/dns4/localhost/tcp/3000/http")
	require.NoError(t, err)

	ctx := t.Context()

	t.Run("queues a location commitment and publishes it in a batch", func(t *testing.T) {
		dstore := dssync.MutexWrap(datastore.NewMapDatastore())
		publisherStore := store.FromDatastore(dstore, store.WithMetadataContext(metadata.MetadataContext))
		svc, queue := newTestService(t, publisherStore, addr)

		space := testutil.RandomDID(t)
		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)

		claimInv := mintLocationClaim(t, space, shard, *location)

		// Publish owes the advertisement, spec included, rather than writing
		// it.
		require.NoError(t, svc.Publish(ctx, claimInv))
		require.Equal(t, []cid.Cid{claimInv.Link()}, queue.links())
		require.NotNil(t, queue.rows[0].Spec, "the row carries what to advertise")
		_, err := publisherStore.Head(ctx)
		require.True(t, store.IsNotFound(err), "nothing is published on the request path")

		// The task publishes what is owed, from the rows alone.
		rows, published, err := svc.PublishClaimed(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, 1, rows)
		require.Equal(t, 1, published)

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
		svc, _ := newTestService(t, publisherStore, addr)

		space := testutil.RandomDID(t)
		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
		claimInv := mintLocationClaim(t, space, shard, *location)
		require.NoError(t, svc.Publish(ctx, claimInv))

		// The first batch writes the advert; the second finds it already
		// advertised and skips it, so a retried batch is harmless.
		_, _, err := svc.PublishClaimed(ctx, 1)
		require.NoError(t, err)
		hd, err := publisherStore.Head(ctx)
		require.NoError(t, err)
		_, _, err = svc.PublishClaimed(ctx, 1)
		require.NoError(t, err)
		again, err := publisherStore.Head(ctx)
		require.NoError(t, err)
		require.Equal(t, hd.Head, again.Head, "an already advertised claim moves nothing")
	})

	t.Run("skips a claim withdrawn before it was published", func(t *testing.T) {
		dstore := dssync.MutexWrap(datastore.NewMapDatastore())
		publisherStore := store.FromDatastore(dstore, store.WithMetadataContext(metadata.MetadataContext))
		svc, _ := newTestService(t, publisherStore, addr)

		space := testutil.RandomDID(t)
		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
		claimInv := mintLocationClaim(t, space, shard, *location)
		require.NoError(t, svc.Publish(ctx, claimInv))

		// Released before the flush: the row is withdrawn, so the batch has
		// nothing for it.
		require.NoError(t, svc.Withdraw(ctx, claimInv.Link()))
		rows, published, err := svc.PublishClaimed(ctx, 1)
		require.NoError(t, err)
		require.Zero(t, rows)
		require.Zero(t, published)
		_, err = publisherStore.Head(ctx)
		require.True(t, store.IsNotFound(err), "no location is advertised for a released blob")
	})

	t.Run("skips a queued row without a spec", func(t *testing.T) {
		dstore := dssync.MutexWrap(datastore.NewMapDatastore())
		publisherStore := store.FromDatastore(dstore, store.WithMetadataContext(metadata.MetadataContext))
		svc, queue := newTestService(t, publisherStore, addr)

		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
		require.NoError(t, svc.Publish(ctx, mintLocationClaim(t, testutil.RandomDID(t), shard, *location)))
		// A row queued before the spec was stored with it.
		queue.rows = append(queue.rows, QueuedAdvert{Claim: testutil.RandomCID(t)})

		rows, published, err := svc.PublishClaimed(ctx, 1)
		require.NoError(t, err, "a row without a spec is skipped, not an error")
		require.Equal(t, 2, rows)
		require.Equal(t, 1, published)
		hd, err := publisherStore.Head(ctx)
		require.NoError(t, err)
		ad, err := publisherStore.Advert(ctx, hd.Head)
		require.NoError(t, err)
		require.Nil(t, ad.PreviousID, "only the row with a spec was advertised")
	})

	t.Run("a withdrawal waits for the batch in flight", func(t *testing.T) {
		dstore := dssync.MutexWrap(datastore.NewMapDatastore())
		publisherStore := store.FromDatastore(dstore, store.WithMetadataContext(metadata.MetadataContext))
		svc, queue := newTestService(t, publisherStore, addr)

		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
		clm := mintLocationClaim(t, testutil.RandomDID(t), shard, *location)
		require.NoError(t, svc.Publish(ctx, clm))

		// Release arrives while the batch is loading its rows under the
		// lock: it must not get in until the batch has committed.
		withdrawn := make(chan error, 1)
		queue.onClaimed = func(ctx context.Context) {
			go func() { withdrawn <- svc.Withdraw(ctx, clm.Link()) }()
			select {
			case err := <-withdrawn:
				t.Errorf("withdrawal completed while the batch held the lock (err=%v)", err)
				// Put it back so the receive below does not hang the test.
				withdrawn <- err
			case <-time.After(50 * time.Millisecond):
			}
		}
		_, _, err := svc.PublishClaimed(ctx, 1)
		require.NoError(t, err)
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

		svc, _ := newTestService(t, publisherStore, addr,
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
