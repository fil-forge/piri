package allocation

import (
	"github.com/fil-forge/ucantone/did"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

// Blob describes the bytes that are allocated.
type Blob struct {
	Digest multihash.Multihash `cborgen:"digest"`
	Size   uint64              `cborgen:"size"`
}

// Allocation is the persisted record of a pre-accepted blob upload slot.
type Allocation struct {
	// Space is the DID of the space this data was allocated for.
	Space did.DID `cborgen:"space"`
	// Blob is the details of the data that was allocated.
	Blob Blob `cborgen:"blob"`
	// Expires is the time (in seconds since unix epoch) at which the
	// allocation becomes invalid and can no longer be accepted.
	Expires uint64 `cborgen:"expires"`
	// Cause is a link to the UCAN invocation that requested the allocation.
	Cause cid.Cid `cborgen:"cause"`
}
