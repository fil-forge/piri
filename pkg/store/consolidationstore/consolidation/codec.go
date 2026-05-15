package consolidation

import (
	"bytes"
	"fmt"
)

// Codec implements genericstore.Codec for Consolidation values using cborgen.
type Codec struct{}

func (Codec) Encode(c Consolidation) ([]byte, error) {
	var buf bytes.Buffer
	if err := c.MarshalCBOR(&buf); err != nil {
		return nil, fmt.Errorf("encoding consolidation: %w", err)
	}
	return buf.Bytes(), nil
}

func (Codec) Decode(data []byte) (Consolidation, error) {
	c := Consolidation{}
	if err := c.UnmarshalCBOR(bytes.NewReader(data)); err != nil {
		return Consolidation{}, fmt.Errorf("decoding consolidation: %w", err)
	}
	return c, nil
}
