//go:build !codegen

package advert

import (
	"bytes"
	"fmt"
)

// Encode produces the CBOR stored in a queue row via the cborgen-generated
// MarshalCBOR method.
func Encode(s Spec) ([]byte, error) {
	var buf bytes.Buffer
	if err := s.MarshalCBOR(&buf); err != nil {
		return nil, fmt.Errorf("encoding advertisement spec: %w", err)
	}
	return buf.Bytes(), nil
}

// Decode parses CBOR produced by Encode.
func Decode(data []byte) (Spec, error) {
	var s Spec
	if err := s.UnmarshalCBOR(bytes.NewReader(data)); err != nil {
		return Spec{}, fmt.Errorf("decoding advertisement spec: %w", err)
	}
	return s, nil
}
