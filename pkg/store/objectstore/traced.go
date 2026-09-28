package objectstore

import (
	"context"
	"errors"
	"io"
	"iter"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/fil-forge/piri/pkg/store/objectstore"

// Traced wraps s so each operation is an objectstore.<op> span naming the
// store, whatever the backend. A backend that makes network calls (MinIO)
// records them as child spans of these.
func Traced(name string, s Store) Store {
	return &tracedStore{name: name, inner: s}
}

// TracedListable is Traced for a ListableStore, keeping Exists and
// ListPrefix available and traced.
func TracedListable(name string, s ListableStore) ListableStore {
	return &tracedListableStore{tracedStore: tracedStore{name: name, inner: s}, inner: s}
}

type tracedStore struct {
	name  string
	inner Store
}

func (s *tracedStore) start(ctx context.Context, op string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	attrs = append(attrs, attribute.String("objectstore.name", s.name))
	// Looked up per call, so the span follows whichever tracer provider is
	// current rather than the one set when this package was loaded.
	return otel.Tracer(tracerName).Start(ctx, "objectstore."+op, trace.WithAttributes(attrs...))
}

func end(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

func (s *tracedStore) Put(ctx context.Context, key string, size uint64, data io.Reader) (err error) {
	ctx, span := s.start(ctx, "put", attribute.Int64("objectstore.size", int64(size)))
	defer func() { end(span, err) }()
	return s.inner.Put(ctx, key, size, data)
}

// Get's span covers opening the object; reading its body happens afterwards,
// in the caller.
func (s *tracedStore) Get(ctx context.Context, key string, opts ...GetOption) (_ Object, err error) {
	ctx, span := s.start(ctx, "get")
	defer func() {
		// A missing object is an answer, not a failure.
		if errors.Is(err, ErrNotExist) {
			span.SetAttributes(attribute.Bool("objectstore.found", false))
			end(span, nil)
			return
		}
		end(span, err)
	}()
	obj, err := s.inner.Get(ctx, key, opts...)
	if err == nil {
		span.SetAttributes(
			attribute.Bool("objectstore.found", true),
			attribute.Int64("objectstore.size", obj.Size()),
		)
	}
	return obj, err
}

func (s *tracedStore) Delete(ctx context.Context, key string) (err error) {
	ctx, span := s.start(ctx, "delete")
	defer func() { end(span, err) }()
	return s.inner.Delete(ctx, key)
}

type tracedListableStore struct {
	tracedStore
	inner ListableStore
}

func (s *tracedListableStore) Exists(ctx context.Context, key string) (_ bool, err error) {
	ctx, span := s.start(ctx, "exists")
	defer func() { end(span, err) }()
	found, err := s.inner.Exists(ctx, key)
	span.SetAttributes(attribute.Bool("objectstore.found", found))
	return found, err
}

// ListPrefix's span runs from the first key requested until iteration stops,
// so it includes the time the caller spends on each key.
func (s *tracedListableStore) ListPrefix(ctx context.Context, prefix string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		ctx, span := s.start(ctx, "list")
		keys := 0
		var err error
		defer func() {
			span.SetAttributes(attribute.Int("objectstore.keys", keys))
			end(span, err)
		}()
		for key, kerr := range s.inner.ListPrefix(ctx, prefix) {
			if kerr != nil {
				err = kerr
			} else {
				keys++
			}
			if !yield(key, kerr) {
				return
			}
		}
	}
}
