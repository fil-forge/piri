package publisher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/digestutil"
	"github.com/fil-forge/libforge/testutil"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/storage/memstore"
	selectorparse "github.com/ipld/go-ipld-prime/traversal/selector/parse"
	"github.com/ipni/go-libipni/dagsync/ipnisync"
	"github.com/labstack/echo/v4"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/lib"
	"github.com/fil-forge/piri/pkg/database/harmonytx"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/store/ipnistore"
	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/objectstore/memory"
)

// An indexer syncing the chain the task published on the Postgres store gets
// every advert and its identity entries chunk.
func TestPostgresStore_IndexerSyncsPublishedChain(t *testing.T) {
	ctx := t.Context()
	env := newPostgresPublisher(t, memory.NewStore())
	env.publishClaims(t, 2)
	require.NoError(t, env.publish(t))

	srv := testutil.Must(NewServer(env.store))(t)
	e := echo.New()
	srv.RegisterRoutes(e)
	httpServer := httptest.NewServer(e)
	t.Cleanup(httpServer.Close)

	hd, err := env.store.Head(ctx)
	require.NoError(t, err)
	var want []cid.Cid
	for lnk := ipld.Link(hd.Head); lnk != nil; {
		ad, err := env.store.Advert(ctx, lnk)
		require.NoError(t, err)
		want = append(want, asCid(lnk), asCid(ad.Entries))
		lnk = ad.PreviousID
	}

	require.ElementsMatch(t, want, syncChain(t, httpServer.URL, env.provider, asCid(hd.Head)))
}

// A batch whose advert cannot be written publishes nothing: the head, the
// maps and the queue rows are as they were, and no indexer hears of it.
func TestPostgresStore_FailedAdvertWriteRollsBack(t *testing.T) {
	failedPublish := func(t *testing.T) (*postgresPublisher, error) {
		t.Helper()
		env := newPostgresPublisher(t, failingPuts{memory.NewStore()})
		env.publishClaims(t, 1)
		return env, env.publish(t)
	}

	t.Run("the publish fails", func(t *testing.T) {
		_, err := failedPublish(t)
		require.ErrorIs(t, err, errPutFailed)
	})

	t.Run("the head does not move", func(t *testing.T) {
		env, _ := failedPublish(t)
		_, err := env.store.Head(t.Context())
		require.True(t, store.IsNotFound(err), "got %v", err)
	})

	t.Run("no context is mapped", func(t *testing.T) {
		env, _ := failedPublish(t)
		require.Zero(t, env.mappedContexts(t))
	})

	t.Run("the queue rows stay claimed", func(t *testing.T) {
		env, _ := failedPublish(t)
		require.Equal(t, 1, env.claimedRows(t))
	})

	t.Run("nothing is announced", func(t *testing.T) {
		env, _ := failedPublish(t)
		require.Zero(t, env.announces())
	})
}

const testBatchID harmonytask.TaskID = 7

type postgresPublisher struct {
	db       *harmonydb.DB
	svc      *PublisherService
	task     *PublishTask
	store    store.FullStore
	provider peer.ID
	announce chan struct{}
}

func newPostgresPublisher(t *testing.T, objects objectstore.Store) *postgresPublisher {
	t.Helper()
	db := piritestutil.NewHarmonyDB(t)
	provider := testutil.Must(peer.IDFromPrivateKey(testutil.Must(lib.Libp2pKey(testutil.Alice))(t)))(t)
	publisherStore := ipnistore.New(db, objects, provider)

	announce := make(chan struct{}, 16)
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		announce <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(indexer.Close)
	indexerURL := testutil.Must(url.Parse(indexer.URL))(t)

	queue := NewDBQueue(db)
	addr := testutil.Must(multiaddr.NewMultiaddr("/dns4/localhost/tcp/3000/http"))(t)
	svc, err := New(testutil.Alice, publisherStore, addr, queue,
		WithTransactor(harmonytx.New(db)), WithDirectAnnounce(*indexerURL))
	require.NoError(t, err)
	return &postgresPublisher{
		db:       db,
		svc:      svc,
		task:     NewPublishTask(queue, svc),
		store:    publisherStore,
		provider: provider,
		announce: announce,
	}
}

func (p *postgresPublisher) publishClaims(t *testing.T, n int) {
	t.Helper()
	for range n {
		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
		require.NoError(t, p.svc.Publish(t.Context(), mintLocationClaim(t, testutil.RandomDID(t), shard, *location)))
	}
	stamped, err := p.db.BeginTransaction(t.Context(), func(tx *harmonydb.Tx) (bool, error) {
		return claimBatch(tx, testBatchID, publishBatchSize)
	})
	require.NoError(t, err)
	require.True(t, stamped)
}

// publish runs the task on the claimed batch.
func (p *postgresPublisher) publish(t *testing.T) error {
	t.Helper()
	_, err := p.task.Do(testBatchID, func() bool { return true })
	return err
}

func (p *postgresPublisher) announces() int {
	return len(p.announce)
}

type countRow struct {
	N int `db:"n"`
}

func (p *postgresPublisher) mappedContexts(t *testing.T) int {
	t.Helper()
	var rows []countRow
	require.NoError(t, p.db.Select(context.Background(), &rows, `SELECT count(*) AS n FROM ipni_contexts`))
	return rows[0].N
}

func (p *postgresPublisher) claimedRows(t *testing.T) int {
	t.Helper()
	var rows []countRow
	require.NoError(t, p.db.Select(context.Background(), &rows,
		`SELECT count(*) AS n FROM ipni_pending_adverts WHERE publish_task_id IS NOT NULL`))
	return rows[0].N
}

var errPutFailed = errors.New("put failed")

// failingPuts is an object store whose writes fail.
type failingPuts struct {
	objectstore.Store
}

func (failingPuts) Put(context.Context, string, uint64, io.Reader) error {
	return errPutFailed
}

// syncChain syncs the chain from head through go-libipni's client, as an
// indexer does, and returns the CIDs it fetched in order.
func syncChain(t *testing.T, serverURL string, provider peer.ID, head cid.Cid) []cid.Cid {
	t.Helper()
	u := testutil.Must(url.Parse(serverURL))(t)
	addr := testutil.Must(multiaddr.NewMultiaddr(fmt.Sprintf("/dns4/%s/tcp/%s/http", u.Hostname(), u.Port())))(t)
	lsys := cidlink.DefaultLinkSystem()
	mem := &memstore.Store{}
	lsys.SetReadStorage(mem)
	lsys.SetWriteStorage(mem)
	var synced []cid.Cid
	client := ipnisync.NewSync(lsys, func(_ peer.ID, c cid.Cid) { synced = append(synced, c) })
	syncer := testutil.Must(client.NewSyncer(peer.AddrInfo{ID: provider, Addrs: []multiaddr.Multiaddr{addr}}))(t)
	require.NoError(t, syncer.Sync(t.Context(), head, selectorparse.CommonSelector_ExploreAllRecursively))
	return synced
}

func asCid(lnk ipld.Link) cid.Cid {
	return lnk.(cidlink.Link).Cid
}
