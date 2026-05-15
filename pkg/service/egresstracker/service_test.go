package egresstracker

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"testing"

	contentcaps "github.com/fil-forge/libforge/capabilities/content"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/receipt"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/client/receipts"
	piritutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/store/consolidationstore"
	"github.com/fil-forge/piri/pkg/store/local/retrievaljournal"
)

func TestAddReceipt(t *testing.T) {
	thisNode := testutil.RandomSigner(t)

	batchEndpoint, err := url.Parse("http://storage.node/receipts/{cid}")
	require.NoError(t, err)

	t.Run("enqueues an egress track task on full batches", func(t *testing.T) {
		tempDir := t.TempDir()
		journal, err := retrievaljournal.NewFSJournal(tempDir, 100) // 100 bytes batch size
		require.NoError(t, err)
		queue := NewMockEgressTrackerQueue(t)

		consolidationStore := consolidationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))

		port := piritutil.GetFreePort(t)
		receiptsEndpoint := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:%d/receipts", port)))(t)
		rcptsClient := receipts.NewClient(receiptsEndpoint)

		service, err := New(
			thisNode,
			testutil.Service.DID(),
			nil, // egressTrackerConn — outbound RPC stubbed (ErrNotMigrated)
			nil, // egressTrackerProofs — outbound RPC stubbed (ErrNotMigrated)
			batchEndpoint,
			journal,
			consolidationStore,
			queue,
			rcptsClient,
			0, // cleanup disabled for tests
		)
		require.NoError(t, err)

		// Test adding a receipt. Max batch size is 100 bytes; this should
		// trigger a batch rotation. The MockEgressTrackerQueue invokes the
		// registered egressTrack function, which is stubbed to return
		// ErrNotMigrated. AddReceipt should surface that error.
		rcpt := createTestReceipt(t, testutil.Alice, thisNode)
		err = service.AddReceipt(t.Context(), rcpt)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrNotMigrated)

		require.Len(t, queue.BatchCIDs(), 1, "expected one batch CID enqueued")
	})

	t.Run("concurrent addition", func(t *testing.T) {
		tempDir := t.TempDir()
		journal, err := retrievaljournal.NewFSJournal(tempDir, 1024)
		require.NoError(t, err)
		queue := NewMockEgressTrackerQueue(t)

		consolidationStore := consolidationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))

		port := piritutil.GetFreePort(t)
		receiptsEndpoint := testutil.Must(url.Parse(fmt.Sprintf("http://localhost:%d/receipts", port)))(t)
		rcptsClient := receipts.NewClient(receiptsEndpoint)

		service, err := New(
			thisNode,
			testutil.Service.DID(),
			nil,
			nil,
			batchEndpoint,
			journal,
			consolidationStore,
			queue,
			rcptsClient,
			0,
		)
		require.NoError(t, err)

		var wg sync.WaitGroup
		numReceipts := 10

		for range numReceipts {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rcpt := createTestReceipt(t, testutil.Alice, thisNode)
				// AddReceipt may return ErrNotMigrated when a batch rotates;
				// either outcome is valid here.
				err := service.AddReceipt(t.Context(), rcpt)
				if err != nil {
					require.ErrorIs(t, err, ErrNotMigrated)
				}
			}()
		}

		wg.Wait()
	})
}

func createTestReceipt(t *testing.T, client ucan.Signer, node ucan.Signer) *receipt.Receipt {
	space := testutil.RandomDID(t)
	inv, err := contentcaps.Retrieve.Invoke(
		client,
		space,
		&contentcaps.RetrieveArguments{
			Blob: contentcaps.Blob{Digest: testutil.RandomMultihash(t)},
		},
		invocation.WithAudience(node.DID()),
	)
	require.NoError(t, err)

	rcpt, err := receipt.IssueOK(node, inv.Link(), &contentcaps.RetrieveOK{})
	require.NoError(t, err)
	return rcpt
}

// MockEgressTrackerQueue invokes the registered egress track function
// synchronously upon enqueue and records each batch CID seen.
type MockEgressTrackerQueue struct {
	t  *testing.T
	mu sync.Mutex

	fn        func(ctx context.Context, batchCID cid.Cid) error
	batchCIDs []cid.Cid
}

func NewMockEgressTrackerQueue(t *testing.T) *MockEgressTrackerQueue {
	return &MockEgressTrackerQueue{t: t}
}

func (m *MockEgressTrackerQueue) Register(fn func(ctx context.Context, batchCID cid.Cid) error) error {
	m.fn = fn
	return nil
}

func (m *MockEgressTrackerQueue) Enqueue(ctx context.Context, batchCID cid.Cid) error {
	m.mu.Lock()
	m.batchCIDs = append(m.batchCIDs, batchCID)
	fn := m.fn
	m.mu.Unlock()

	if fn == nil {
		return errors.New("no enqueue function registered")
	}
	return fn(ctx, batchCID)
}

func (m *MockEgressTrackerQueue) BatchCIDs() []cid.Cid {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]cid.Cid{}, m.batchCIDs...)
}
