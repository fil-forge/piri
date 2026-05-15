package publisher

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/fil-forge/go-libstoracha/ipnipublisher/store"
	"github.com/fil-forge/go-libstoracha/metadata"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"

	"github.com/fil-forge/libforge/capabilities"
	"github.com/fil-forge/libforge/capabilities/assert"
	"github.com/fil-forge/libforge/digestutil"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/service/publisher/advertisement"
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

		claim, err := assert.Location.Invoke(
			testutil.Alice,
			space,
			&assert.LocationArguments{
				Space:    space,
				Content:  shard,
				Location: []capabilities.CborURL{capabilities.CborURL(*location)},
			},
			invocation.WithAudience(testutil.Alice.DID()),
			invocation.WithNoExpiration(),
		)
		require.NoError(t, err)

		err = svc.Publish(ctx, claim)
		require.NoError(t, err)

		hd, err := publisherStore.Head(ctx)
		require.NoError(t, err)

		ad, err := publisherStore.Advert(ctx, hd.Head)
		require.NoError(t, err)

		require.Equal(
			t,
			testutil.Must(advertisement.EncodeContextID(space, shard))(t),
			ad.ContextID,
		)

		meta := metadata.MetadataContext.New()
		err = meta.UnmarshalBinary(ad.Metadata)
		require.NoError(t, err)

		protocol := meta.Get(metadata.LocationCommitmentID)
		require.NotNil(t, protocol)

		lcmeta, ok := protocol.(*metadata.LocationCommitmentMetadata)
		require.True(t, ok)

		require.Equal(t, claim.Link().String(), lcmeta.Claim.String())

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

		claim, err := assert.Location.Invoke(
			testutil.Alice,
			space,
			&assert.LocationArguments{
				Space:    space,
				Content:  shard,
				Location: []capabilities.CborURL{capabilities.CborURL(*location)},
			},
			invocation.WithAudience(testutil.Alice.DID()),
			invocation.WithNoExpiration(),
		)
		require.NoError(t, err)

		err = svc.Publish(ctx, claim)
		require.NoError(t, err)

		err = svc.Publish(ctx, claim)
		require.NoError(t, err)
	})
}
