package publisher

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/fil-forge/go-ipni-tools/pkg/metadata"
	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/digestutil"
	"github.com/fil-forge/libforge/testutil"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/ipld/go-ipld-prime"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
)

// TestPublishTask_PublishesClaimedBatch runs the task against harmonydb:
// queued claims are stamped the way IAmBored stamps them, Do publishes the
// batch under one commit and retires its rows. It skips where no postgres
// container is available.
func TestPublishTask_PublishesClaimedBatch(t *testing.T) {
	db := piritestutil.NewHarmonyDB(t)
	ctx := t.Context()

	queue := NewDBQueue(db)
	publisherStore := store.FromDatastore(dssync.MutexWrap(datastore.NewMapDatastore()),
		store.WithMetadataContext(metadata.MetadataContext))
	claims := invocationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
	addr := testutil.Must(multiaddr.NewMultiaddr("/dns4/localhost/tcp/3000/http"))(t)
	svc, err := New(testutil.Alice, publisherStore, addr, queue, claims)
	require.NoError(t, err)
	task := NewPublishTask(queue, svc)

	queueClaim := func() cid.Cid {
		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
		clm := mintLocationClaim(t, testutil.RandomDID(t), shard, *location)
		require.NoError(t, claims.Put(ctx, clm))
		require.NoError(t, queue.Enqueue(ctx, clm.Link()))
		return clm.Link()
	}

	// Three accepted blobs owe an advertisement; a fourth is released before
	// it is published, and a fifth is accepted again (the same claim twice).
	const published = 3
	var links []cid.Cid
	for range published {
		links = append(links, queueClaim())
	}
	released := queueClaim()
	require.NoError(t, claims.Delete(ctx, released))
	require.NoError(t, queue.Dequeue(ctx, released))
	require.NoError(t, queue.Enqueue(ctx, links[0]), "re-queueing a claim is a no-op")

	// The backlog the metrics report: three waiting, the oldest for no time
	// worth speaking of yet.
	count, oldest, err := queue.pending(ctx)
	require.NoError(t, err)
	require.EqualValues(t, published, count)
	require.GreaterOrEqual(t, oldest, time.Duration(0))
	require.Less(t, oldest, time.Minute)

	// Claim the batch the way IAmBored does.
	const id harmonytask.TaskID = 7
	stamped, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		return claimBatch(tx, id, publishBatchSize)
	})
	require.NoError(t, err)
	require.True(t, stamped)
	claimed, err := queue.claimed(ctx, id)
	require.NoError(t, err)
	require.ElementsMatch(t, links, claimed, "the released claim is not in the batch")

	// Claimed rows still count as pending: a batch that is retrying must not
	// vanish from the backlog gauges.
	count, _, err = queue.pending(ctx)
	require.NoError(t, err)
	require.EqualValues(t, published, count, "claimed rows are still pending")

	// A worker the engine has taken the task from publishes nothing and
	// leaves the rows for the new owner.
	done, err := task.Do(id, func() bool { return false })
	require.ErrorIs(t, err, errLostOwnership)
	require.False(t, done)
	_, err = publisherStore.Head(ctx)
	require.True(t, store.IsNotFound(err), "a worker that lost the task must not publish")
	claimed, err = queue.claimed(ctx, id)
	require.NoError(t, err)
	require.Len(t, claimed, published, "the rows stay stamped for the task's new owner")

	done, err = task.Do(id, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)

	// One chain of three advertisements behind one head.
	hd, err := publisherStore.Head(ctx)
	require.NoError(t, err)
	var chain int
	for lnk := ipld.Link(hd.Head); lnk != nil; chain++ {
		ad, err := publisherStore.Advert(ctx, lnk)
		require.NoError(t, err)
		lnk = ad.PreviousID
	}
	require.Equal(t, published, chain)

	// The batch's rows are retired and nothing is left to claim.
	var rows []struct {
		N int `db:"n"`
	}
	require.NoError(t, db.Select(ctx, &rows, `SELECT count(*) AS n FROM ipni_pending_adverts`))
	require.Zero(t, rows[0].N)
	count, oldest, err = queue.pending(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	require.Zero(t, oldest, "an empty queue has no backlog age")
	stamped, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		return claimBatch(tx, id+1, publishBatchSize)
	})
	require.NoError(t, err)
	require.False(t, stamped, "an empty queue claims nothing")
}

// TestPublishTask_SchedulesThroughTheEngine pins that a flush schedules
// through the add function the engine hands over, and not before it has: the
// ticker starts with the app, the engine somewhat later.
func TestPublishTask_SchedulesThroughTheEngine(t *testing.T) {
	task := NewPublishTask(nil, nil)

	// No add function yet: scheduling must wait rather than run without one.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	task.schedule(ctx)

	var adds int
	task.Adder(func(func(harmonytask.TaskID, *harmonydb.Tx) (bool, error)) { adds++ })
	task.schedule(t.Context())
	task.schedule(t.Context())
	require.Equal(t, 2, adds, "every flush asks the engine for one task")
}

// TestDBQueue_ReclaimsOrphanedRows pins that rows stamped by a task the
// engine no longer has go back to the pool, since claimBatch only takes
// unclaimed rows and would otherwise never see them again.
func TestDBQueue_ReclaimsOrphanedRows(t *testing.T) {
	db := piritestutil.NewHarmonyDB(t)
	ctx := t.Context()
	queue := NewDBQueue(db)

	links := []cid.Cid{testutil.RandomCID(t), testutil.RandomCID(t)}
	for _, l := range links {
		require.NoError(t, queue.Enqueue(ctx, l))
	}
	// Stamped by a task id that is not in harmony_task, as after the engine
	// deleted a task that failed too often.
	_, err := db.Exec(ctx, `UPDATE ipni_pending_adverts SET publish_task_id = 424242`)
	require.NoError(t, err)
	stamped, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		return claimBatch(tx, 7, publishBatchSize)
	})
	require.NoError(t, err)
	require.False(t, stamped, "nothing is unclaimed while the rows are stranded")

	n, err := queue.reclaimOrphans(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	stamped, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		return claimBatch(tx, 7, publishBatchSize)
	})
	require.NoError(t, err)
	require.True(t, stamped, "reclaimed rows are claimable again")
	claimed, err := queue.claimed(ctx, 7)
	require.NoError(t, err)
	require.ElementsMatch(t, links, claimed)
}
