package serializer

import (
	"bytes"
	"encoding/json"
	"fmt"

	cbg "github.com/whyrusleeping/cbor-gen"
)

type Serializer[T any] interface {
	Serialize(val T) ([]byte, error)
	Deserialize(data []byte) (T, error)
}

type JSON[T any] struct{}

func (J JSON[T]) Serialize(val T) ([]byte, error) {
	return json.Marshal(val)
}

func (J JSON[T]) Deserialize(data []byte) (T, error) {
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
}

// CBOR is a Serializer for types whose pointer satisfies cbor-gen's
// MarshalCBOR/UnmarshalCBOR (the generated method set produced by
// github.com/whyrusleeping/cbor-gen).
//
// The cbor-gen interface assertion is checked at runtime on the first
// Serialize/Deserialize — we don't enforce it via a type constraint to
// keep bootstrap clean (a cbor_gen.go tagged `//go:build !codegen` is
// excluded from the codegen build, so a compile-time `*T` constraint
// would deadlock against the generated methods).
//
// Usage:
//
//	type MyJob struct {
//	    Foo cid.Cid `cborgen:"foo"`
//	}
//	// (cbor_gen.go generates MarshalCBOR/UnmarshalCBOR on *MyJob)
//
//	q, err := jobqueue.New[MyJob](
//	    name, db,
//	    serializer.CBOR[MyJob]{},
//	    ...
//	)
type CBOR[T any] struct{}

func (c CBOR[T]) Serialize(val T) ([]byte, error) {
	m, ok := any(&val).(cbg.CBORMarshaler)
	if !ok {
		return nil, fmt.Errorf("type %T does not implement cbg.CBORMarshaler", val)
	}
	var buf bytes.Buffer
	if err := m.MarshalCBOR(&buf); err != nil {
		return nil, fmt.Errorf("cbor-gen marshal: %w", err)
	}
	return buf.Bytes(), nil
}

func (c CBOR[T]) Deserialize(data []byte) (T, error) {
	var v T
	u, ok := any(&v).(cbg.CBORUnmarshaler)
	if !ok {
		return v, fmt.Errorf("type %T does not implement cbg.CBORUnmarshaler", v)
	}
	if err := u.UnmarshalCBOR(bytes.NewReader(data)); err != nil {
		return v, fmt.Errorf("cbor-gen unmarshal: %w", err)
	}
	return v, nil
}
