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
	// mu guards both i and actualBatches. Both are written on the rotator's
	// goroutine and read by the Eventually condition on a third.
	var mu sync.Mutex
	i := 0
	rj := mockRetrievalJournal{
		forceRotateFunc: func() (bool, cid.Cid, error) {
			mu.Lock()
			defer mu.Unlock()
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
	// without one the poll races the appends and -race catches it -- measured
	// at 60 failures in 60 independent runs.
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
	// Wait for BOTH: every fixture entry presented to the rotator, and every
	// real one collected. The drain half is what keeps the fixture and the
	// assertion from drifting apart -- add an entry the rotator can never
	// reach and this times out and says so, rather than passing on a mock it
	// only half consumed.
	//
	// Waiting on length alone is not enough, and an earlier revision's attempt
	// to catch that AFTER Stop() was itself a race: with a trailing cid.Undef
	// appended to batches, whether the rotator got one more tick before Stop()
	// took effect decided whether the check fired. Measured at 97 catches in
	// 120 runs -- a guard that is right 80% of the time, which is the shape
	// rule 5 warns about. Waiting on the drain makes it deterministic instead.
	//
	// assert, not require: require aborts this goroutine, so pr.Stop() below
	// would never run. On a FAILING condition that is not merely an untidy
	// leak -- a failing condition means the mock is still undrained, so the
	// rotator is still producing, and it ticks into t.Logf after the test has
	// returned until Go kills the binary with "Log in goroutine after
	// TestPeriodicRotator has completed". Reproduced with a deadline short
	// enough to fail, 10 processes out of 10; with the 2s deadline below the
	// condition succeeds and nothing aborts, so the panic needs that
	// precondition stated to be a true claim.
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return i == len(batches) && len(actualBatches) == len(expectedBatches)
	}, 2*time.Second, time.Millisecond)
	err := pr.Stop(t.Context())
	require.NoError(t, err)

	// Restates what the condition waited for, so a timeout reports WHICH half
	// was missing instead of only the batch-list diff. Stop() has joined the
	// rotator goroutine by here -- run() does `defer close(r.stopped)` and
	// Stop() blocks on <-r.stopped -- but the lock is taken anyway, because
	// the ctx.Err() path in Stop() returns without that join.
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, len(batches), i, "the rotator did not consume every mock batch")
	require.Equal(t, expectedBatches, actualBatches)
}

type mockRetrievalJournal struct {
	forceRotateFunc func() (bool, cid.Cid, error)
}

func (m *mockRetrievalJournal) ForceRotate(ctx context.Context) (bool, cid.Cid, error) {
	return m.forceRotateFunc()
}
