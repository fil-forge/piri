package publisher

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/fil-forge/go-ipni-tools/pkg/metadata"
	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/digestutil"
	"github.com/fil-forge/libforge/testutil"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/announce/message"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestPublishClaimedAnnouncesTheNewHead(t *testing.T) {
	ctx := t.Context()
	announced := make(chan cid.Cid, 4)
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg message.Message
		if err := msg.UnmarshalCBOR(r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		announced <- msg.Cid
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(indexer.Close)
	indexerURL := testutil.Must(url.Parse(indexer.URL))(t)

	addr := testutil.Must(multiaddr.NewMultiaddr("/dns4/localhost/tcp/3000/http"))(t)
	publisherStore := store.FromDatastore(dssync.MutexWrap(datastore.NewMapDatastore()),
		store.WithMetadataContext(metadata.MetadataContext), store.WithIdentityEntries())
	svc, _ := newTestService(t, publisherStore, addr, WithDirectAnnounce(*indexerURL))

	shard := testutil.RandomMultihash(t)
	location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
	require.NoError(t, svc.Publish(ctx, mintLocationClaim(t, testutil.RandomDID(t), shard, *location)))
	_, _, err := svc.PublishClaimed(ctx, 1)
	require.NoError(t, err)
	hd, err := publisherStore.Head(ctx)
	require.NoError(t, err)
	close(announced)

	var got []cid.Cid
	for c := range announced {
		got = append(got, c)
	}
	require.Equal(t, []cid.Cid{hd.Head.(cidlink.Link).Cid}, got)
}
