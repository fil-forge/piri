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
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

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

// Piri's telemetry carries the Forge namespace alongside its own name, which is
// what selects Forge's services together in Alloy and Grafana.
func TestSetupResourceNamespace(t *testing.T) {
	// With no collector configured the tracer provider is a no-op one, whose
	// spans carry no resource.
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	t.Cleanup(func() { require.NoError(t, tel.Shutdown(ctx)) })

	_, span := otel.Tracer("test").Start(ctx, "root")
	defer span.End()
	ro, ok := span.(sdktrace.ReadOnlySpan)
	require.True(t, ok, "span is not an SDK span")

	attrs := ro.Resource().Set()
	ns, ok := attrs.Value(semconv.ServiceNamespaceKey)
	require.True(t, ok, "resource has no service.namespace")
	require.Equal(t, "forge", ns.AsString())
	name, ok := attrs.Value(semconv.ServiceNameKey)
	require.True(t, ok, "resource has no service.name")
	require.Equal(t, "piri", name.AsString())
}
