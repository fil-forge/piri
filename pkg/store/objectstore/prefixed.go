package objectstore

import (
	"context"
	"io"
)

// Prefixed returns s with prefix prepended to every key, so several kinds of
// object can share one bucket.
func Prefixed(prefix string, s Store) Store {
	return &prefixedStore{prefix: prefix, inner: s}
}

type prefixedStore struct {
	prefix string
	inner  Store
}

func (s *prefixedStore) Put(ctx context.Context, key string, size uint64, data io.Reader) error {
	return s.inner.Put(ctx, s.prefix+key, size, data)
}

func (s *prefixedStore) Get(ctx context.Context, key string, opts ...GetOption) (Object, error) {
	return s.inner.Get(ctx, s.prefix+key, opts...)
}

func (s *prefixedStore) Delete(ctx context.Context, key string) error {
	return s.inner.Delete(ctx, s.prefix+key)
}
