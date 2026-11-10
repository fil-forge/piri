package publisher

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/fil-forge/go-ipni-tools/pkg/metadata"
	ipnipub "github.com/fil-forge/go-ipni-tools/pkg/publisher"
	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/digestutil"
	"github.com/fil-forge/libforge/testutil"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/ipld/go-ipld-prime"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
)

// A batch whose rows include one a release has withdrawn but not yet
// committed waits for the release, then leaves that row out: a location for
// a released blob is never published.
func TestPublishClaimed_WaitsForRelease(t *testing.T) {
	db := piritestutil.NewHarmonyDB(t)
	ctx := t.Context()
	tx := harmonytx.New(db)
	queue := NewDBQueue(db)
	publisherStore := store.FromDatastore(dssync.MutexWrap(datastore.NewMapDatastore()),
		store.WithMetadataContext(metadata.MetadataContext))
	addr := testutil.Must(multiaddr.NewMultiaddr("/dns4/localhost/tcp/3000/http"))(t)
	svc, err := New(testutil.Alice, publisherStore, addr, queue, WithTransactor(tx))
	require.NoError(t, err)

	queueClaim := func() cid.Cid {
		shard := testutil.RandomMultihash(t)
		location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
		clm := mintLocationClaim(t, testutil.RandomDID(t), shard, *location)
		require.NoError(t, svc.Publish(ctx, clm))
		return clm.Link()
	}
	released := queueClaim()
	queueClaim()

	const id harmonytask.TaskID = 9
	stamped, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		return claimBatch(tx, id, publishBatchSize)
	})
	require.NoError(t, err)
	require.True(t, stamped)

	withdrawn, finish := make(chan struct{}), make(chan struct{})
	releaseDone := make(chan error, 1)
	go func() {
		releaseDone <- tx.Run(ctx, func(ctx context.Context) error {
			if err := svc.Withdraw(ctx, released); err != nil {
				return err
			}
			close(withdrawn)
			<-finish
			return nil
		})
	}()
	<-withdrawn

	type result struct{ rows, published int }
	publishDone := make(chan result, 1)
	publishErr := make(chan error, 1)
	go func() {
		rows, published, err := svc.PublishClaimed(ctx, id)
		publishErr <- err
		publishDone <- result{rows, published}
	}()
	select {
	case <-publishErr:
		t.Fatal("the batch loaded its rows while a release of one of them was uncommitted")
	case <-time.After(200 * time.Millisecond):
	}

	close(finish)
	require.NoError(t, <-releaseDone)
	require.NoError(t, <-publishErr)
	got := <-publishDone
	require.Equal(t, 1, got.rows, "the released claim is not in the batch")
	require.Equal(t, 1, got.published)

	// The other claim was published and its row retired with the batch; the
	// released one was dequeued by the release.
	claimed, err := queue.Claimed(ctx, id)
	require.NoError(t, err)
	require.Empty(t, claimsOf(claimed))
}

// blockingBatchPublisher holds a batch mid-publish until release is closed.
type blockingBatchPublisher struct {
	ipnipub.BatchPublisher
	publishing chan struct{}
	release    chan struct{}
}

func (b *blockingBatchPublisher) PublishBatch(ctx context.Context, provider peer.AddrInfo, specs []ipnipub.AdvertSpec) (ipld.Link, error) {
	close(b.publishing)
	<-b.release
	return b.BatchPublisher.PublishBatch(ctx, provider, specs)
}

// A batch that has loaded its rows holds them until it has published: a
// deletion of one of them waits for it, whether or not it goes through the
// publisher's own lock, as a delete from another process would not.
func TestPublishClaimed_HoldsRowsWhilePublishing(t *testing.T) {
	db := piritestutil.NewHarmonyDB(t)
	ctx := t.Context()
	queue := NewDBQueue(db)
	publisherStore := store.FromDatastore(dssync.MutexWrap(datastore.NewMapDatastore()),
		store.WithMetadataContext(metadata.MetadataContext))
	addr := testutil.Must(multiaddr.NewMultiaddr("/dns4/localhost/tcp/3000/http"))(t)
	svc, err := New(testutil.Alice, publisherStore, addr, queue, WithTransactor(harmonytx.New(db)))
	require.NoError(t, err)
	blocking := &blockingBatchPublisher{BatchPublisher: svc.ipni, publishing: make(chan struct{}), release: make(chan struct{})}
	svc.ipni = blocking

	shard := testutil.RandomMultihash(t)
	location := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:3000/blob/%s", digestutil.Format(shard))))(t)
	clm := mintLocationClaim(t, testutil.RandomDID(t), shard, *location)
	require.NoError(t, svc.Publish(ctx, clm))
	const id harmonytask.TaskID = 11
	stamped, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		return claimBatch(tx, id, publishBatchSize)
	})
	require.NoError(t, err)
	require.True(t, stamped)

	published := make(chan error, 1)
	go func() {
		_, _, err := svc.PublishClaimed(ctx, id)
		published <- err
	}()
	<-blocking.publishing

	dequeued := make(chan error, 1)
	go func() { dequeued <- queue.Dequeue(ctx, clm.Link()) }()
	select {
	case err := <-dequeued:
		t.Fatalf("the row was deleted while its batch was publishing it: %v", err)
	case <-time.After(time.Second):
	}

	close(blocking.release)
	require.NoError(t, <-published)
	require.NoError(t, <-dequeued)
}
