//go:build !codegen

package acceptance

import (
	"bytes"
	"fmt"
)

// Encode produces a self-contained CBOR envelope for the acceptance via
// the cborgen-generated MarshalCBOR method.
func Encode(a Acceptance) ([]byte, error) {
	var buf bytes.Buffer
	if err := a.MarshalCBOR(&buf); err != nil {
		return nil, fmt.Errorf("encoding acceptance: %w", err)
	}
	return buf.Bytes(), nil
}

// Decode parses a CBOR envelope produced by Encode.
func Decode(data []byte) (Acceptance, error) {
	var a Acceptance
	if err := a.UnmarshalCBOR(bytes.NewReader(data)); err != nil {
		return Acceptance{}, fmt.Errorf("decoding acceptance: %w", err)
	}
	return a, nil
}

// Codec implements genericstore.Codec for Acceptance values.
type Codec struct{}

func (Codec) Encode(a Acceptance) ([]byte, error) { return Encode(a) }

func (Codec) Decode(data []byte) (Acceptance, error) { return Decode(data) }
