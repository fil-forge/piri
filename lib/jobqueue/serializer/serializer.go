package serializer

import (
	"encoding/json"
	"io"

	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
)

// Codec encodes and decodes job payloads of type T to and from the queue's
// wire format. It is supplied as a value when constructing a JobQueue, which
// keeps the choice of serialization format out of the queue's type parameter:
// CBOR and dag-json payload types coexist behind the same Codec[T] interface.
type Codec[T any] interface {
	Encode(w io.Writer, v T) error
	Decode(r io.Reader) (T, error)
}

// CBORable is the method set emitted by whyrusleeping/cbor-gen. The generated
// methods have pointer receivers, so it is satisfied by *U, never by U.
type CBORable interface {
	cbg.CBORMarshaler
	cbg.CBORUnmarshaler
}

// cborPtr binds a value type U to its pointer *U, letting Decode allocate a
// fresh value before unmarshalling into it.
type cborPtr[U any] interface {
	*U
	CBORable
}

// CBOR returns a Codec for a cbor-gen type. Call it as CBOR[PieceLink](); the
// pointer payload type T (= *PieceLink) is inferred from the constraint's core
// type, so callers never spell it out.
func CBOR[U any, T cborPtr[U]]() Codec[T] {
	return cborCodec[U, T]{}
}

type cborCodec[U any, T cborPtr[U]] struct{}

func (cborCodec[U, T]) Encode(w io.Writer, v T) error {
	return v.MarshalCBOR(w)
}

func (cborCodec[U, T]) Decode(r io.Reader) (T, error) {
	// new(U) is required: UnmarshalCBOR has a pointer receiver and would panic
	// on the nil pointer that `var v T` produces for a pointer type T.
	v := T(new(U))
	if err := v.UnmarshalCBOR(r); err != nil {
		return nil, err
	}
	return v, nil
}

// DagJSONMarshaler and DagJSONUnmarshaler are the method set emitted by
// alanshaw/dag-json-gen. Like cbor-gen, the generated methods have pointer
// receivers and operate over io.Writer/io.Reader.
type DagJSONMarshaler interface {
	MarshalDagJSON(w io.Writer) error
}

type DagJSONUnmarshaler interface {
	UnmarshalDagJSON(r io.Reader) error
}

// dagJSONPtr binds a value type U to its pointer *U for dag-json types.
type dagJSONPtr[U any] interface {
	*U
	DagJSONMarshaler
	DagJSONUnmarshaler
}

// DagJSON returns a Codec for a dag-json-gen type. Call it as DagJSON[PieceLink]().
func DagJSON[U any, T dagJSONPtr[U]]() Codec[T] {
	return dagJSONCodec[U, T]{}
}

type dagJSONCodec[U any, T dagJSONPtr[U]] struct{}

func (dagJSONCodec[U, T]) Encode(w io.Writer, v T) error {
	return v.MarshalDagJSON(w)
}

func (dagJSONCodec[U, T]) Decode(r io.Reader) (T, error) {
	v := T(new(U))
	if err := v.UnmarshalDagJSON(r); err != nil {
		return nil, err
	}
	return v, nil
}

// CID returns a Codec for cid.Cid payloads, using the CID binary form. Use it
// for job queues keyed directly by a CID rather than by a cbor-gen struct.
func CID() Codec[cid.Cid] { return cidCodec{} }

type cidCodec struct{}

func (cidCodec) Encode(w io.Writer, c cid.Cid) error {
	_, err := c.WriteBytes(w)
	return err
}

func (cidCodec) Decode(r io.Reader) (cid.Cid, error) {
	_, c, err := cid.CidFromReader(r)
	return c, err
}

// JSON returns a Codec backed by encoding/json. Use it for payload types that
// carry hand-written or reflection-based JSON marshalling rather than a
// cbor-gen / dag-json-gen codec.
func JSON[T any]() Codec[T] { return jsonCodec[T]{} }

type jsonCodec[T any] struct{}

func (jsonCodec[T]) Encode(w io.Writer, v T) error {
	return json.NewEncoder(w).Encode(v)
}

func (jsonCodec[T]) Decode(r io.Reader) (T, error) {
	var v T
	err := json.NewDecoder(r).Decode(&v)
	return v, err
}
