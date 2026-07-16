package acceptance

import (
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

// Acceptance is the record persisted per accepted blob.
type Acceptance struct {
	// Space is the DID of the space this data was accepted for.
	Space did.DID `cborgen:"space"`
	// Blob is the details of the data that was accepted.
	Blob Blob `cborgen:"blob"`
	// PDPAccept is the promise of the `/pdp/accept` task completion.
	// Nil for acceptances that did not enqueue PDP aggregation.
	PDPAccept *promise.AwaitOK `cborgen:"pdpAccept,omitempty"`
	// ExecutedAt is the approximate time (in seconds since unix epoch) that
	// the `/blob/accept` invocation was executed.
	ExecutedAt uint64 `cborgen:"executedAt"`
	// Cause is a link to the `/blob/accept` task that requested the
	// acceptance.
	Cause cid.Cid `cborgen:"cause"`
	// Claim is a link to the `/assert/location` claim minted at acceptance —
	// the digest→claim index `/blob/remove` uses to delete the location
	// claim when this space's acceptance is removed. Nil for acceptances
	// recorded before this field existed.
	Claim *cid.Cid `cborgen:"claim,omitempty"`
}

// Blob captures the bytes the acceptance attests to.
type Blob struct {
	Digest multihash.Multihash `cborgen:"digest"`
	Size   uint64              `cborgen:"size"`
}
