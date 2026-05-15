package advertisement

import (
	"bytes"

	"github.com/fil-forge/ucantone/did"
	mh "github.com/multiformats/go-multihash"
)

// Encode canonically encodes ContextID data.
func EncodeContextID(space did.DID, digest mh.Multihash) ([]byte, error) {
	return mh.Sum(bytes.Join([][]byte{[]byte(space.String()), digest}, nil), mh.SHA2_256, -1)
}
