package advertisement

import (
	"bytes"

	"github.com/fil-forge/go-ucanto/did"
	mh "github.com/multiformats/go-multihash"
)

// Encode canonically encodes ContextID data.
func EncodeContextID(space did.DID, digest mh.Multihash) ([]byte, error) {
	return mh.Sum(bytes.Join([][]byte{space.Bytes(), digest}, nil), mh.SHA2_256, -1)
}
