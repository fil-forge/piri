package telemetry

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrorHandler(t *testing.T) {
	var logged []uint64
	h := newErrorHandler()
	h.log = func(_ error, occurrences uint64) { logged = append(logged, occurrences) }

	t.Run("backs off on a repeated error", func(t *testing.T) {
		logged = nil
		for range 20 {
			h.Handle(errors.New("failed to upload metrics: no such host"))
		}
		require.Equal(t, []uint64{1, 2, 4, 8, 16}, logged)
	})

	t.Run("a different error starts a new count", func(t *testing.T) {
		logged = nil
		h.Handle(errors.New("failed to upload metrics: connection refused"))
		h.Handle(errors.New("failed to upload metrics: connection refused"))
		h.Handle(errors.New("failed to upload metrics: connection refused"))
		require.Equal(t, []uint64{1, 2}, logged)
	})

	t.Run("ignores nil", func(t *testing.T) {
		logged = nil
		h.Handle(nil)
		require.Empty(t, logged)
	})
}
