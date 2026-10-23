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
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/fil-forge/piri/pkg/store/ipnistore"
)

func TestPublishClaimedAnnouncesTheNewHead(t *testing.T) {
	ctx := t.Context()
	announced := make(chan cid.Cid, 4)
	svc, publisherStore := newAnnouncingService(t, announced)
	publishOneClaim(t, svc)
	hd, err := publisherStore.Head(ctx)
	require.NoError(t, err)
	close(announced)

	var got []cid.Cid
	for c := range announced {
		got = append(got, c)
	}
	require.Equal(t, []cid.Cid{hd.Head.(cidlink.Link).Cid}, got)
}

// The announce is traced once, so both checks read the same recording: the
// global tracer provider delegates only to the first one a test sets.
func TestPublishClaimedTracesTheAnnounce(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	svc, _ := newAnnouncingService(t, make(chan cid.Cid, 4))
	publishOneClaim(t, svc)

	var announceSpan sdktrace.ReadOnlySpan
	for _, sp := range rec.Ended() {
		if sp.Name() == "ipni.announce" {
			announceSpan = sp
		}
	}
	require.NotNil(t, announceSpan, "no ipni.announce span")

	// Each announce request is a client span under ipni.announce, so a
	// trace shows how long every indexer took.
	t.Run("each indexer request is a child span", func(t *testing.T) {
		var children []string
		for _, sp := range rec.Ended() {
			if sp.Parent().SpanID() == announceSpan.SpanContext().SpanID() {
				children = append(children, sp.Name())
			}
		}
		require.Equal(t, []string{"HTTP PUT"}, children)
	})

	// The head CID is unique per batch, so it stays out of span attributes.
	t.Run("the span carries no attributes", func(t *testing.T) {
		require.Empty(t, announceSpan.Attributes())
	})
}

// newAnnouncingService returns a publisher service that announces to an
// indexer, which sends the CID of every announce it receives to announced.
func newAnnouncingService(t *testing.T, announced chan<- cid.Cid) (*PublisherService, store.PublisherStore) {
	t.Helper()
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
		store.WithMetadataContext(metadata.MetadataContext), store.WithIdentityEntries(ipnistore.MaxIdentityEntries))
	svc, _ := newTestService(t, publisherStore, addr, WithDirectAnnounce(*indexerURL))
	return svc, publisherStore
}

// publishOneClaim queues one location claim and publishes it.
func publishOneClaim(t *testing.T, svc *PublisherService) {
	t.Helper()
	shard := testutil.RandomMultihash(t)
	location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
	require.NoError(t, svc.Publish(t.Context(), mintLocationClaim(t, testutil.RandomDID(t), shard, *location)))
	_, _, err := svc.PublishClaimed(t.Context(), 1)
	require.NoError(t, err)
}
