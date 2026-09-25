package telemetry

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type loggedError struct {
	msg         string
	occurrences uint64
}

func newTestErrorHandler() (*errorHandler, *[]loggedError, *time.Time) {
	var logged []loggedError
	clock := time.Unix(0, 0)
	h := newErrorHandler()
	h.log = func(err error, occurrences uint64) {
		logged = append(logged, loggedError{err.Error(), occurrences})
	}
	h.now = func() time.Time { return clock }
	return h, &logged, &clock
}

func TestErrorHandler(t *testing.T) {
	noHost := errors.New("failed to upload metrics: no such host")
	refused := errors.New("failed to upload traces: connection refused")

	t.Run("backs off on a repeated error", func(t *testing.T) {
		h, logged, _ := newTestErrorHandler()
		for range 20 {
			h.Handle(noHost)
		}
		require.Equal(t, []loggedError{
			{noHost.Error(), 1}, {noHost.Error(), 2}, {noHost.Error(), 4},
			{noHost.Error(), 8}, {noHost.Error(), 16},
		}, *logged)
	})

	t.Run("interleaved errors back off independently", func(t *testing.T) {
		h, logged, _ := newTestErrorHandler()
		for range 8 {
			h.Handle(noHost)
			h.Handle(refused)
		}
		require.Equal(t, []loggedError{
			{noHost.Error(), 1}, {refused.Error(), 1},
			{noHost.Error(), 2}, {refused.Error(), 2},
			{noHost.Error(), 4}, {refused.Error(), 4},
			{noHost.Error(), 8}, {refused.Error(), 8},
		}, *logged)
	})

	t.Run("an error starts again after a quiet period", func(t *testing.T) {
		h, logged, clock := newTestErrorHandler()
		for range 3 {
			h.Handle(noHost)
		}
		*clock = clock.Add(errorResetAfter + time.Second)
		h.Handle(noHost)
		require.Equal(t, []loggedError{
			{noHost.Error(), 1}, {noHost.Error(), 2}, {noHost.Error(), 1},
		}, *logged)
	})

	t.Run("keeps backing off within the quiet period", func(t *testing.T) {
		h, logged, clock := newTestErrorHandler()
		for range 3 {
			h.Handle(noHost)
			*clock = clock.Add(30 * time.Second)
		}
		h.Handle(noHost)
		require.Equal(t, []loggedError{
			{noHost.Error(), 1}, {noHost.Error(), 2}, {noHost.Error(), 4},
		}, *logged)
	})

	t.Run("bounds the errors it tracks", func(t *testing.T) {
		h, _, _ := newTestErrorHandler()
		for i := range maxTrackedErrors * 3 {
			h.Handle(fmt.Errorf("error %d", i))
		}
		require.LessOrEqual(t, len(h.errors), maxTrackedErrors)
	})

	t.Run("a full map keeps the backoff of a recurring error", func(t *testing.T) {
		h, logged, clock := newTestErrorHandler()
		for i := range maxTrackedErrors * 4 {
			*clock = clock.Add(time.Millisecond)
			h.Handle(fmt.Errorf("error %d", i))
			*clock = clock.Add(time.Millisecond)
			h.Handle(noHost)
		}
		var noHostLogged []uint64
		for _, l := range *logged {
			if l.msg == noHost.Error() {
				noHostLogged = append(noHostLogged, l.occurrences)
			}
		}
		require.Equal(t, []uint64{1, 2, 4, 8, 16, 32, 64, 128, 256}, noHostLogged)
		require.LessOrEqual(t, len(h.errors), maxTrackedErrors)
	})

	t.Run("ignores nil", func(t *testing.T) {
		h, logged, _ := newTestErrorHandler()
		h.Handle(nil)
		require.Empty(t, *logged)
	})
}
