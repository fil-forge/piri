package telemetry

import (
	"sync"
	"time"
)

const (
	// errorResetAfter is how long an error has to go unreported before its
	// count starts again, so the first failure after a recovery is logged.
	// Several publish intervals, so a collector that is down stays backed off.
	errorResetAfter = 5 * time.Minute

	// maxTrackedErrors bounds the counts kept at once. Errors whose text varies
	// from one export to the next would otherwise grow the map without limit.
	maxTrackedErrors = 64
)

// errorHandler receives the errors the OpenTelemetry SDK reports on its own,
// such as a failed periodic export, and logs them through Piri's logger. The
// SDK's default handler prints them with the standard library logger, which
// gives them no level and no logger name.
//
// A collector that stays unreachable fails every export, so each distinct
// error is logged with exponential backoff: on its 1st, 2nd, 4th, 8th and so
// on occurrence. Counts are kept per error, so two collectors failing in
// different ways each back off, and an error unreported for errorResetAfter
// starts again from one.
type errorHandler struct {
	log func(err error, occurrences uint64)
	now func() time.Time

	mu     sync.Mutex
	errors map[string]*trackedError
}

type trackedError struct {
	count    uint64
	lastSeen time.Time
}

func newErrorHandler() *errorHandler {
	return &errorHandler{
		log: func(err error, occurrences uint64) {
			log.Warnw("telemetry error", "error", err, "occurrences", occurrences)
		},
		now:    time.Now,
		errors: map[string]*trackedError{},
	}
}

func (h *errorHandler) Handle(err error) {
	if err == nil {
		return
	}

	msg := err.Error()
	now := h.now()

	h.mu.Lock()
	for k, e := range h.errors {
		if now.Sub(e.lastSeen) > errorResetAfter {
			delete(h.errors, k)
		}
	}
	e, ok := h.errors[msg]
	if !ok {
		if len(h.errors) >= maxTrackedErrors {
			clear(h.errors)
		}
		e = &trackedError{}
		h.errors[msg] = e
	}
	e.count++
	e.lastSeen = now
	n := e.count
	h.mu.Unlock()

	// Powers of two only.
	if n&(n-1) != 0 {
		return
	}
	h.log(err, n)
}
