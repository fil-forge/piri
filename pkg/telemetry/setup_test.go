package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/fil-forge/piri/pkg/config/app"
)

// A request that arrives without a trace of its own must still be traced: no
// upstream starts traces for Piri yet, so a sampler that only follows a parent
// records nothing at all.
func TestSetupTracesRootSpans(t *testing.T) {
	var exports atomic.Int32
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/traces" {
			exports.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(collector.Close)

	ctx := context.Background()
	tel, err := Setup(ctx, "", "did:key:test", app.TelemetryConfig{
		Environment: "test",
		Traces: []app.TelemetryCollectorConfig{{
			Endpoint: strings.TrimPrefix(collector.URL, "http://"),
			Insecure: true,
		}},
	})
	require.NoError(t, err)

	_, span := otel.Tracer("test").Start(ctx, "root")
	require.True(t, span.SpanContext().IsSampled(), "a root span must be sampled")
	span.End()

	// Shutdown flushes the batch processor.
	require.NoError(t, tel.Shutdown(ctx))
	require.Positive(t, exports.Load(), "the root span never reached the collector")
}
