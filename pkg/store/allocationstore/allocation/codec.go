package allocation

import (
	"bytes"
	"fmt"
)

// Codec implements genericstore.Codec for Allocation values using cborgen.
type Codec struct{}

func (Codec) Encode(a Allocation) ([]byte, error) {
	var buf bytes.Buffer
	if err := a.MarshalCBOR(&buf); err != nil {
		return nil, fmt.Errorf("encoding allocation: %w", err)
	}
	return buf.Bytes(), nil
}

func (Codec) Decode(data []byte) (Allocation, error) {
	a := Allocation{}
	if err := a.UnmarshalCBOR(bytes.NewReader(data)); err != nil {
		return Allocation{}, fmt.Errorf("decoding allocation: %w", err)
	}
	return a, nil
}
