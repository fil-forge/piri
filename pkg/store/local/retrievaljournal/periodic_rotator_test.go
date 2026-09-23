package retrievaljournal_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fil-forge/libforge/testutil"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store/local/retrievaljournal"
)

func TestPeriodicRotator(t *testing.T) {
	batches := []cid.Cid{
		testutil.RandomCID(t),
		cid.Undef, // signal for no rotation due to empty batch
		testutil.RandomCID(t),
		testutil.RandomCID(t),
		testutil.RandomCID(t),
		cid.Undef,
		testutil.RandomCID(t),
		testutil.RandomCID(t),
	}
	i := 0
	rj := mockRetrievalJournal{
		forceRotateFunc: func() (bool, cid.Cid, error) {
			if i >= len(batches) {
				return false, cid.Undef, nil
			}
			batch := batches[i]
			rotated := batch != cid.Undef
			i++
			return rotated, batch, nil
		},
	}
	pr := retrievaljournal.NewPeriodicRotator(&rj, time.Millisecond)

	var expectedBatches []cid.Cid
	for _, batch := range batches {
		if batch != cid.Undef {
			expectedBatches = append(expectedBatches, batch)
		}
	}

	// collect the rotation batches. RotateFunc runs on the rotator's own
	// goroutine and Eventually polls from a third, so the slice needs a lock:
	// without one the poll races the appends and -race fails every run.
	var mu sync.Mutex
	actualBatches := []cid.Cid{}
	pr.RotateFunc = func(batchID cid.Cid) {
		mu.Lock()
		actualBatches = append(actualBatches, batchID)
		mu.Unlock()
		t.Logf("Rotated batch: %s", batchID)
	}

	pr.Start()
	// A deadline, not a fixed sleep. The rotator ticks every millisecond, so
	// sleeping 30ms and asserting six rotations is a bet on how much CPU the
	// goroutine gets: measured at 2 failures in 480 runs under GOMAXPROCS=1
	// with 24 concurrent test processes, reporting three and five of six.
	//
	// assert, not require. require aborts this goroutine, so pr.Stop() below
	// never runs -- and the consequence is not a leak, it is a panic: the
	// rotator keeps ticking into t.Logf after the test returns, and Go kills
	// the binary with "Log in goroutine after TestPeriodicRotator has
	// completed". Measured at -count=5, every run.
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(actualBatches) == len(expectedBatches)
	}, 2*time.Second, time.Millisecond)
	err := pr.Stop(t.Context())
	require.NoError(t, err)

	// The condition above keys on LENGTH, which means "the mock was fully
	// consumed" only because the last fixture entry happens to be a real CID.
	// Append a cid.Undef to batches and the test would stop exercising the
	// tail without any assertion noticing. Check the drain directly.
	//
	// Reading i here is race-free: Stop() joins the rotator goroutine --
	// run() does `defer close(r.stopped)` and Stop() blocks on <-r.stopped --
	// so nothing is still calling forceRotateFunc.
	require.Equal(t, len(batches), i, "the rotator did not consume every mock batch")

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, expectedBatches, actualBatches)
}

type mockRetrievalJournal struct {
	forceRotateFunc func() (bool, cid.Cid, error)
}

func (m *mockRetrievalJournal) ForceRotate(ctx context.Context) (bool, cid.Cid, error) {
	return m.forceRotateFunc()
}
