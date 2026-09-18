package telemetry

import (
	"sync"
	"time"

	"go.opentelemetry.io/otel"
)

// defaultErrorLogInterval is the shortest gap between two lines the
// OpenTelemetry error handler writes. An exporter that cannot reach its
// collector fails once per publish interval for as long as the node runs, so
// without this a single misconfiguration fills the log.
const defaultErrorLogInterval = 5 * time.Minute

var setErrorHandlerOnce sync.Once

// SetErrorHandler routes the OpenTelemetry SDK's own errors through Piri's
// logger, rate limited.
//
// The SDK's default handler writes them with the standard library's log
// package, which gives them no level and no logger name, so they are invisible
// to level-based log queries.
//
// Only the first call takes effect: otel.SetErrorHandler delegates errors to
// the handler given to it the first time, and later calls replace the handler
// without that delegation.
func SetErrorHandler() {
	setErrorHandlerOnce.Do(func() {
		otel.SetErrorHandler(newRateLimitedErrorHandler(defaultErrorLogInterval))
	})
}

// rateLimitedErrorHandler logs at most one error per interval, and reports how
// many it dropped in between so a repeating fault is still visible as one.
type rateLimitedErrorHandler struct {
	interval time.Duration
	// logError is the sink, replaced in tests.
	logError func(err error, suppressed int)

	mu         sync.Mutex
	next       time.Time
	suppressed int
}

func newRateLimitedErrorHandler(interval time.Duration) *rateLimitedErrorHandler {
	return &rateLimitedErrorHandler{
		interval: interval,
		logError: func(err error, suppressed int) {
			if suppressed > 0 {
				log.Warnw("opentelemetry error", "error", err, "suppressed", suppressed)
				return
			}
			log.Warnw("opentelemetry error", "error", err)
		},
	}
}

func (h *rateLimitedErrorHandler) Handle(err error) {
	if err == nil {
		return
	}

	h.mu.Lock()
	now := time.Now()
	if now.Before(h.next) {
		h.suppressed++
		h.mu.Unlock()
		return
	}
	suppressed := h.suppressed
	h.suppressed = 0
	h.next = now.Add(h.interval)
	h.mu.Unlock()

	h.logError(err, suppressed)
}
