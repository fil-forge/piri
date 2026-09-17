package telemetry

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type loggedError struct {
	err        error
	suppressed int
}

func newTestHandler(interval time.Duration) (*rateLimitedErrorHandler, *[]loggedError) {
	var (
		mu     sync.Mutex
		logged []loggedError
	)
	h := newRateLimitedErrorHandler(interval)
	h.logError = func(err error, suppressed int) {
		mu.Lock()
		defer mu.Unlock()
		logged = append(logged, loggedError{err: err, suppressed: suppressed})
	}
	return h, &logged
}

func TestRateLimitedErrorHandlerLogsFirstErrorImmediately(t *testing.T) {
	h, logged := newTestHandler(time.Hour)

	h.Handle(errors.New("failed to upload metrics"))

	require.Len(t, *logged, 1)
	require.EqualError(t, (*logged)[0].err, "failed to upload metrics")
	require.Zero(t, (*logged)[0].suppressed)
}

func TestRateLimitedErrorHandlerCountsSuppressedErrors(t *testing.T) {
	h, logged := newTestHandler(time.Hour)

	h.Handle(errors.New("first"))
	for range 5 {
		h.Handle(errors.New("suppressed"))
	}
	require.Len(t, *logged, 1, "only the first error is logged within the interval")

	// Let the interval lapse: the next error reports what was dropped.
	h.next = time.Now().Add(-time.Second)
	h.Handle(errors.New("next"))

	require.Len(t, *logged, 2)
	require.EqualError(t, (*logged)[1].err, "next")
	require.Equal(t, 5, (*logged)[1].suppressed)
}

func TestRateLimitedErrorHandlerResetsSuppressedCount(t *testing.T) {
	h, logged := newTestHandler(time.Hour)

	h.Handle(errors.New("first"))
	h.Handle(errors.New("suppressed"))
	h.next = time.Now().Add(-time.Second)
	h.Handle(errors.New("second"))
	h.next = time.Now().Add(-time.Second)
	h.Handle(errors.New("third"))

	require.Len(t, *logged, 3)
	require.Equal(t, 1, (*logged)[1].suppressed)
	require.Zero(t, (*logged)[2].suppressed, "the count covers one interval, not the run")
}

func TestRateLimitedErrorHandlerIgnoresNil(t *testing.T) {
	h, logged := newTestHandler(time.Hour)

	h.Handle(nil)

	require.Empty(t, *logged)
}
