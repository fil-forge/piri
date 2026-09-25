package telemetry

import (
	"sync"
)

// errorHandler receives the errors the OpenTelemetry SDK reports on its own,
// such as a failed periodic export, and logs them through Piri's logger. The
// SDK's default handler prints them with the standard library logger, which
// gives them no level and no logger name.
//
// A collector that stays unreachable fails every export, so a repeat of the
// previous error is logged with exponential backoff: on its 1st, 2nd, 4th, 8th
// and so on occurrence. A different error resets the count.
type errorHandler struct {
	log func(err error, occurrences uint64)

	mu    sync.Mutex
	last  string
	count uint64
}

func newErrorHandler() *errorHandler {
	return &errorHandler{
		log: func(err error, occurrences uint64) {
			log.Warnw("telemetry error", "error", err, "occurrences", occurrences)
		},
	}
}

func (h *errorHandler) Handle(err error) {
	if err == nil {
		return
	}

	msg := err.Error()
	h.mu.Lock()
	if msg != h.last {
		h.last = msg
		h.count = 0
	}
	h.count++
	n := h.count
	h.mu.Unlock()

	// Powers of two only.
	if n&(n-1) != 0 {
		return
	}
	h.log(err, n)
}
