package publisher

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

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
)

func TestPublisherService(t *testing.T) {
	addr, err := multiaddr.NewMultiaddr("/dns4/localhost/tcp/3000/http")
	require.NoError(t, err)

	ctx := t.Context()

	t.Run("publishes location commitments", func(t *testing.T) {
		dstore := dssync.MutexWrap(datastore.NewMapDatastore())
		publisherStore := store.FromDatastore(dstore, store.WithMetadataContext(metadata.MetadataContext))

		svc, err := New(testutil.Alice, publisherStore, addr)
		require.NoError(t, err)

		space := testutil.RandomDID(t)
		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)

		claimInv := mintLocationClaim(t, space, shard, *location)

		require.NoError(t, svc.Publish(ctx, claimInv))

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

		svc, err := New(testutil.Alice, publisherStore, addr)
		require.NoError(t, err)

		space := testutil.RandomDID(t)
		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
		claimInv := mintLocationClaim(t, space, shard, *location)

		// First publish writes the advert; the second hits the
		// ipnipub.ErrAlreadyAdvertised path which the publisher swallows.
		require.NoError(t, svc.Publish(ctx, claimInv))
		require.NoError(t, svc.Publish(ctx, claimInv))
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

		svc, err := New(
			testutil.Alice,
			publisherStore,
			addr,
			WithIndexingService(app.IndexingServiceConfig{
				DID:    testutil.Bob.DID(),
				Client: httpClient,
			}),
			WithIndexingServiceProof([]ucan.Delegation{proof}),
		)
		require.NoError(t, err)

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
