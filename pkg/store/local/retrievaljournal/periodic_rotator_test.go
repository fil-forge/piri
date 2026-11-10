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
	// would never run and the rotator is never stopped.
	//
	// THE PANIC THAT CAUSES NEEDS TWO PRECONDITIONS, and two revisions of this
	// comment each stated only one. It needs a FAILING condition -- with the
	// 2s deadline below the condition succeeds and require aborts nothing --
	// AND a still-running rotator, which then ticks into t.Logf after the test
	// has returned until Go kills the binary with "Log in goroutine after
	// TestPeriodicRotator has completed". It also needs -count=5 or more:
	// TestPeriodicRotator is the last test in this package, so at -count=1 the
	// process exits before the stray log lands. Measured: 0 panics in 10 at
	// -count=1, 10 in 10 at -count=5.
	//
	// "Undrained implies still producing" does NOT follow, which an earlier
	// revision asserted. In the one fixture-drift case the drain half exists
	// for -- an entry the rotator can never reach -- the rotator is stuck
	// rather than producing, and require aborts with no panic at all: 0 in 10
	// at -count=5, against 10 in 10 for a rotator still running.
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return i == len(batches) && len(actualBatches) == len(expectedBatches)
	}, 2*time.Second, time.Millisecond)
	err := pr.Stop(t.Context())
	require.NoError(t, err)

	// Restates what the condition waited for, so a timeout reports WHICH half
	// was missing instead of only the batch-list diff.
	//
	// The lock here is REDUNDANT, and an earlier revision justified it with a
	// path that cannot be reached: Stop()'s ctx.Err() branch does return
	// without joining, but require.NoError on the line above Goexits first, so
	// execution never arrives here with the goroutine still running. On every
	// path that does arrive, run() has closed r.stopped and Stop() has
	// received it. Kept because every other access to these two variables is
	// locked and an unlocked one here reads as an oversight.
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
