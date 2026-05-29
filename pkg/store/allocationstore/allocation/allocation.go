package allocation

import (
	"bytes"
	// for go:embed
	_ "embed"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/ipfs/go-cid"
)

type Allocation struct {
	// Space is the DID of the space this data was allocated for.
	Space did.DID `cborgen:"space" dagjsongen:"space"`
	// Blob is the details of the data that was allocated.
	Blob blob.Blob `cborgen:"blob" dagjsongen:"blob"`
	// Expires is the time (in seconds since unix epoch) at which the
	// allocation becomes invalid and can no longer be accepted.
	Expires ucan.UnixTimestamp `cborgen:"expires" dagjsongen:"expired"`
	// Cause is a link to the task that requested the allocation.
	Cause cid.Cid `cborgen:"cause" dagjsongen:"cause"`
}

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
