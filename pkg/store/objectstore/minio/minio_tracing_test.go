package minio

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/fil-forge/piri/pkg/store/objectstore"
)

// Object store requests are client spans under the caller's span.
func TestRequestsAreTraced(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	store := createTestStore(t, uniqueBucketName(t.Name()))
	ctx, parent := otel.Tracer("test").Start(context.Background(), "blob.accept")
	data := []byte("traced")
	require.NoError(t, store.Put(ctx, "key", uint64(len(data)), bytes.NewReader(data)))
	exists, err := store.Exists(ctx, "key")
	require.NoError(t, err)
	require.True(t, exists)
	parent.End()

	var children int
	for _, sp := range rec.Ended() {
		if sp.SpanKind() == trace.SpanKindClient && sp.Parent().SpanID() == parent.SpanContext().SpanID() {
			children++
		}
	}
	require.GreaterOrEqual(t, children, 2, "expected a client span per object store request")
}

// Behind the traced wrapper, each HTTP request nests under its operation span.
func TestTracedStoreSpansNestHTTP(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	store := objectstore.TracedListable("allocations", createTestStore(t, uniqueBucketName(t.Name())))
	data := []byte("traced")
	require.NoError(t, store.Put(context.Background(), "key", uint64(len(data)), bytes.NewReader(data)))

	var put sdktrace.ReadOnlySpan
	for _, sp := range rec.Ended() {
		if sp.Name() == "objectstore.put" {
			put = sp
		}
	}
	require.NotNil(t, put)
	var nested bool
	for _, sp := range rec.Ended() {
		if sp.SpanKind() == trace.SpanKindClient && sp.Parent().SpanID() == put.SpanContext().SpanID() {
			nested = true
		}
	}
	require.True(t, nested, "expected the HTTP request under objectstore.put")
}
