package ipldstore

import (
	"bytes"
	"context"
	"fmt"

	"github.com/fil-forge/go-libstoracha/ipnipublisher/store"
	cbg "github.com/whyrusleeping/cbor-gen"
)

// KVStore is a key/value store over a SimpleStore, keyed by any fmt.Stringer
// and holding values that (de)serialize via cbor-gen.
type KVStore[K, V any] interface {
	Get(ctx context.Context, key K) (V, error)
	Put(ctx context.Context, key K, value V) error
}

// cborValue constrains V so that *V can cbor-marshal itself. cbor-gen emits
// pointer-receiver MarshalCBOR/UnmarshalCBOR, so the store works with value V
// externally while marshalling through *V internally.
type cborValue[V any] interface {
	*V
	cbg.CBORMarshaler
	cbg.CBORUnmarshaler
}

type cborStore[K fmt.Stringer, V any, PV cborValue[V]] struct {
	ds store.SimpleStore
}

func (s *cborStore[K, V, PV]) Get(ctx context.Context, key K) (V, error) {
	var zero V
	r, err := s.ds.Get(ctx, key.String())
	if err != nil {
		return zero, err
	}
	defer r.Close()
	v := PV(new(V))
	if err := v.UnmarshalCBOR(r); err != nil {
		return zero, fmt.Errorf("decoding %T: %w", zero, err)
	}
	return *v, nil
}

func (s *cborStore[K, V, PV]) Put(ctx context.Context, key K, value V) error {
	var buf bytes.Buffer
	if err := PV(&value).MarshalCBOR(&buf); err != nil {
		return fmt.Errorf("encoding %T: %w", value, err)
	}
	return s.ds.Put(ctx, key.String(), uint64(buf.Len()), &buf)
}

// CBORStore returns a KVStore that serializes values with cbor-gen. Provide K
// and V explicitly; the pointer type PV is inferred from V.
func CBORStore[K fmt.Stringer, V any, PV cborValue[V]](ds store.SimpleStore) KVStore[K, V] {
	return &cborStore[K, V, PV]{ds: ds}
}
