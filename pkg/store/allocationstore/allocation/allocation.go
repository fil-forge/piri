package allocation

import (
	"bytes"
	// for go:embed
	_ "embed"
	"fmt"

	"github.com/fil-forge/go-libstoracha/capabilities/types"
	"github.com/fil-forge/go-ucanto/core/ipld"
	"github.com/fil-forge/libforge/capabilities/blob"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/ipfs/go-cid"
	ipldprime "github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/codec"
	"github.com/ipld/go-ipld-prime/codec/dagcbor"
	"github.com/ipld/go-ipld-prime/datamodel"
	"github.com/ipld/go-ipld-prime/node/bindnode"
	"github.com/ipld/go-ipld-prime/schema"
)

//go:embed allocation.ipldsch
var allocationSchema []byte

var allocationTS *schema.TypeSystem

func init() {
	ts, err := ipldprime.LoadSchemaBytes(allocationSchema)
	if err != nil {
		panic(fmt.Errorf("loading allocation schema: %w", err))
	}
	allocationTS = ts
}

func AllocationType() schema.Type {
	return allocationTS.TypeByName("Allocation")
}

type Allocation struct {
	// Space is the DID of the space this data was allocated for.
	Space did.DID
	// Blob is the details of the data that was allocated.
	Blob blob.Blob
	// Expires is the time (in seconds since unix epoch) at which the
	// allocation becomes invalid and can no longer be accepted.
	Expires ucan.UnixTimestamp
	// Cause is a link to the UCAN that requested the allocation.
	Cause cid.Cid
}

func (a Allocation) ToIPLD() (datamodel.Node, error) {
	return ipld.WrapWithRecovery(&a, AllocationType(), types.Converters...)
}

func Encode(alloc Allocation, enc codec.Encoder) ([]byte, error) {
	n, err := alloc.ToIPLD()
	if err != nil {
		return nil, fmt.Errorf("encoding to IPLD: %w", err)
	}

	if enc == nil {
		enc = dagcbor.Encode
	}

	buf := bytes.NewBuffer([]byte{})
	err = enc(n, buf)
	if err != nil {
		return nil, fmt.Errorf("encoding to data format: %w", err)
	}

	return buf.Bytes(), nil
}

func Decode(data []byte, dec codec.Decoder) (Allocation, error) {
	if dec == nil {
		dec = dagcbor.Decode
	}

	nb := bindnode.Prototype((*Allocation)(nil), AllocationType(), types.Converters...).NewBuilder()

	err := dec(nb, bytes.NewBuffer(data))
	if err != nil {
		return Allocation{}, fmt.Errorf("decoding from data format: %w", err)
	}

	nd := nb.Build()
	al := bindnode.Unwrap(nd).(*Allocation)
	return *al, nil
}

// Codec implements genericstore.Codec for Allocation values.
type Codec struct{}

func (Codec) Encode(a Allocation) ([]byte, error) {
	return Encode(a, dagcbor.Encode)
}

func (Codec) Decode(data []byte) (Allocation, error) {
	return Decode(data, dagcbor.Decode)
}
