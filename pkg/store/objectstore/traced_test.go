package objectstore_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/objectstore/dsadapter"
)

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return rec
}

func spanNamed(t *testing.T, rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, sp := range rec.Ended() {
		if sp.Name() == name {
			return sp
		}
	}
	t.Fatalf("no %q span", name)
	return nil
}

func TestTracedListable(t *testing.T) {
	rec := recordSpans(t)
	store := objectstore.TracedListable("allocations", dsadapter.New(dssync.MutexWrap(datastore.NewMapDatastore())))
	ctx, parent := otel.Tracer("test").Start(context.Background(), "blob.allocate")

	data := []byte("hello")
	require.NoError(t, store.Put(ctx, "a/1", uint64(len(data)), bytes.NewReader(data)))
	require.NoError(t, store.Put(ctx, "a/2", uint64(len(data)), bytes.NewReader(data)))

	obj, err := store.Get(ctx, "a/1")
	require.NoError(t, err)
	got, err := io.ReadAll(obj.Body())
	require.NoError(t, err)
	require.Equal(t, data, got)

	_, err = store.Get(ctx, "missing")
	require.ErrorIs(t, err, objectstore.ErrNotExist)

	found, err := store.Exists(ctx, "a/2")
	require.NoError(t, err)
	require.True(t, found)

	var keys []string
	for k, err := range store.ListPrefix(ctx, "a/") {
		require.NoError(t, err)
		keys = append(keys, k)
	}
	require.Len(t, keys, 2)
	parent.End()

	byName := map[string][]sdktrace.ReadOnlySpan{}
	for _, sp := range rec.Ended() {
		if sp.Name() == "blob.allocate" {
			continue
		}
		require.Equal(t, parent.SpanContext().SpanID(), sp.Parent().SpanID(), sp.Name())
		require.Contains(t, sp.Attributes(), attribute.String("objectstore.name", "allocations"), sp.Name())
		byName[sp.Name()] = append(byName[sp.Name()], sp)
	}
	require.Len(t, byName["objectstore.put"], 2)
	require.Contains(t, byName["objectstore.put"][0].Attributes(), attribute.Int64("objectstore.size", 5))
	require.Len(t, byName["objectstore.get"], 2)
	hit, miss := byName["objectstore.get"][0], byName["objectstore.get"][1]
	require.Contains(t, hit.Attributes(), attribute.Bool("objectstore.found", true))
	require.Contains(t, miss.Attributes(), attribute.Bool("objectstore.found", false))
	require.Equal(t, codes.Unset, miss.Status().Code, "a missing object is not a failure")
	require.Contains(t, spanNamed(t, rec, "objectstore.exists").Attributes(), attribute.Bool("objectstore.found", true))
	require.Contains(t, spanNamed(t, rec, "objectstore.list").Attributes(), attribute.Int("objectstore.keys", 2))
}

// Stopping iteration early still ends the list span.
func TestTracedListStopsEarly(t *testing.T) {
	rec := recordSpans(t)
	store := objectstore.TracedListable("receipts", dsadapter.New(dssync.MutexWrap(datastore.NewMapDatastore())))
	for _, k := range []string{"k/1", "k/2", "k/3"} {
		require.NoError(t, store.Put(context.Background(), k, 1, bytes.NewReader([]byte("x"))))
	}
	for range store.ListPrefix(context.Background(), "k/") {
		break
	}
	require.Contains(t, spanNamed(t, rec, "objectstore.list").Attributes(), attribute.Int("objectstore.keys", 1))
}
