package allocation

import (
	// for go:embed
	_ "embed"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
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

// Pending is an allocation made without a digest: the `/blob/allocate` named
// only the code of the hash function, and the digest is computed as the data
// is received. It is identified by Allocation, the `/blob/allocate` task link,
// until then. Once the data has been received the allocation also counts as a
// claim on (Digest, Space) through an [Allocation] record.
type Pending struct {
	// Allocation is the link to the `/blob/allocate` task that created it.
	Allocation cid.Cid `cborgen:"allocation" dagjsongen:"allocation"`
	// Space is the DID of the space this data was allocated for.
	Space did.DID `cborgen:"space" dagjsongen:"space"`
	// Size is the number of bytes allocated.
	Size uint64 `cborgen:"size" dagjsongen:"size"`
	// DigestCode is the multicodec code of the hash function the data is
	// hashed with as it is received.
	DigestCode uint64 `cborgen:"digestCode" dagjsongen:"digestCode"`
	// Cause is a link to the `/blob/add` task that requested the allocation.
	Cause cid.Cid `cborgen:"cause" dagjsongen:"cause"`
	// Expires is the time (in seconds since unix epoch) at which the
	// allocation becomes invalid and can no longer be accepted.
	Expires ucan.UnixTimestamp `cborgen:"expires" dagjsongen:"expires"`
	// UploadID identifies the upload the data is received by, and its staged
	// bytes until they move to the key of their digest.
	UploadID string `cborgen:"uploadID" dagjsongen:"uploadID"`
	// Digest is the digest computed as the data was received. It is empty
	// until the upload completes.
	Digest multihash.Multihash `cborgen:"digest,omitempty" dagjsongen:"digest,omitempty"`
	// Accepted records that `/blob/accept` committed this allocation.
	Accepted bool `cborgen:"accepted" dagjsongen:"accepted"`
}
