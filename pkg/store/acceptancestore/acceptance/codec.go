package acceptance

import (
	"bytes"
	"fmt"
)

// Codec implements genericstore.Codec for Acceptance values using cborgen.
type Codec struct{}

func (Codec) Encode(a Acceptance) ([]byte, error) {
	var buf bytes.Buffer
	if err := a.MarshalCBOR(&buf); err != nil {
		return nil, fmt.Errorf("encoding acceptance: %w", err)
	}
	return buf.Bytes(), nil
}

func (Codec) Decode(data []byte) (Acceptance, error) {
	a := Acceptance{}
	if err := a.UnmarshalCBOR(bytes.NewReader(data)); err != nil {
		return Acceptance{}, fmt.Errorf("decoding acceptance: %w", err)
	}
	return a, nil
}
