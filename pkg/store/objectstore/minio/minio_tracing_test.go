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
