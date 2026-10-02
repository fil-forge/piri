//go:build !codegen

package allocation

import "bytes"

// Codec implements genericstore.Codec for Allocation values.
type Codec struct{}

func (Codec) Encode(a Allocation) ([]byte, error) {
	out := new(bytes.Buffer)
	if err := a.MarshalCBOR(out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (Codec) Decode(data []byte) (Allocation, error) {
	out := new(Allocation)
	if err := out.UnmarshalCBOR(bytes.NewReader(data)); err != nil {
		return Allocation{}, err
	}
	return *out, nil
}

// PendingCodec implements genericstore.Codec for Pending values.
type PendingCodec struct{}

func (PendingCodec) Encode(p Pending) ([]byte, error) {
	out := new(bytes.Buffer)
	if err := p.MarshalCBOR(out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (PendingCodec) Decode(data []byte) (Pending, error) {
	out := new(Pending)
	if err := out.UnmarshalCBOR(bytes.NewReader(data)); err != nil {
		return Pending{}, err
	}
	return *out, nil
}
