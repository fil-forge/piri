// Package ipldstore provides a small KV-store adapter that round-trips
// cborgen-marshalable values to a SimpleStore.
//
// Historically this package was bindnode/ipld-prime backed; aggregation
// was its only caller and has since migrated to cborgen, so the
// IPLDStore/schema.Type API is gone. The package name is kept for
// import-path stability.
package ipldstore

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/fil-forge/go-libstoracha/ipnipublisher/store"
	cbg "github.com/whyrusleeping/cbor-gen"
)

// KVStore is the typed get/put surface a CBORStore exposes.
type KVStore[K, V any] interface {
	Get(ctx context.Context, key K) (V, error)
	Put(ctx context.Context, key K, value V) error
}

type cborStore[K fmt.Stringer, V any] struct {
	ds store.SimpleStore
}

func (s *cborStore[K, V]) Get(ctx context.Context, key K) (V, error) {
	var zero V
	r, err := s.ds.Get(ctx, key.String())
	if err != nil {
		return zero, err
	}
	defer r.Close()

	var v V
	u, ok := any(&v).(cbg.CBORUnmarshaler)
	if !ok {
		return zero, fmt.Errorf("type %T does not implement cbg.CBORUnmarshaler", v)
	}
	if err := u.UnmarshalCBOR(r); err != nil {
		return zero, fmt.Errorf("decoding %s: %w", key.String(), err)
	}
	return v, nil
}

func (s *cborStore[K, V]) Put(ctx context.Context, key K, value V) error {
	m, ok := any(&value).(cbg.CBORMarshaler)
	if !ok {
		return fmt.Errorf("type %T does not implement cbg.CBORMarshaler", value)
	}
	var buf bytes.Buffer
	if err := m.MarshalCBOR(&buf); err != nil {
		return fmt.Errorf("encoding %s: %w", key.String(), err)
	}
	return s.ds.Put(ctx, key.String(), uint64(buf.Len()), io.NopCloser(&buf))
}

// CBORStore wires a SimpleStore underneath a generic KV API where
// values must satisfy cbor-gen's MarshalCBOR/UnmarshalCBOR via their
// pointer type.
//
// The cbor-gen interface assertion is checked at runtime on the first
// Get/Put — we don't enforce it via a type constraint to keep
// bootstrap clean (a cbor_gen.go tagged `//go:build !codegen` is
// excluded from the codegen build, so a compile-time PV constraint
// would create a chicken-and-egg between the store constructor and the
// generated methods).
//
// Call sites:
//
//	ipldstore.CBORStore[cid.Cid, Aggregate](simpleStore)
func CBORStore[K fmt.Stringer, V any](ds store.SimpleStore) KVStore[K, V] {
	return &cborStore[K, V]{ds: ds}
}
