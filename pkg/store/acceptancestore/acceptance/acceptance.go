package acceptance

import (
	"github.com/fil-forge/ucantone/did"
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
	PDPAccept *Promise `cborgen:"pdpAccept,omitempty"`
	// ExecutedAt is the approximate time (in seconds since unix epoch) that
	// the `/blob/accept` invocation was executed.
	ExecutedAt uint64 `cborgen:"executedAt"`
	// Cause is a link to the `/blob/accept` invocation that requested the
	// acceptance.
	Cause cid.Cid `cborgen:"cause"`
}

// Blob captures the bytes the acceptance attests to.
type Blob struct {
	Digest multihash.Multihash `cborgen:"digest"`
	Size   uint64              `cborgen:"size"`
}

// Await wraps the selector + task link of a UCAN await reference.
type Await struct {
	Selector string  `cborgen:"selector"`
	Link     cid.Cid `cborgen:"link"`
}

// Promise wraps a UCAN await reference under the "ucan/await" tag.
type Promise struct {
	UcanAwait Await `cborgen:"ucan/await"`
}
