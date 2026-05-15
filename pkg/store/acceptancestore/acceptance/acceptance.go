package acceptance

import (
	"github.com/fil-forge/ucantone/did"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

// Acceptance is the persisted record of a successful blob acceptance.
type Acceptance struct {
	// Space is the DID of the space this data was accepted for.
	Space did.DID `cborgen:"space"`
	// Blob is the details of the data that was accepted.
	Blob Blob `cborgen:"blob"`
	// PDPAccept is the promise of the `/pdp/accept` task completion, set only
	// when PDP is enabled.
	PDPAccept *Promise `cborgen:"pdpAccept,omitempty"`
	// ExecutedAt is the approximate time (in seconds since unix epoch) that
	// the `/blob/accept` invocation was executed.
	ExecutedAt uint64 `cborgen:"executedAt"`
	// Cause is a link to the `/blob/accept` invocation that requested the
	// acceptance.
	Cause cid.Cid `cborgen:"cause"`
}

// Blob describes the bytes that were accepted.
type Blob struct {
	Digest multihash.Multihash `cborgen:"digest"`
	Size   uint64              `cborgen:"size"`
}

// Promise wraps a single `ucan/await` reference.
type Promise struct {
	UcanAwait Await `cborgen:"ucan/await"`
}

// Await references a task by selector and link.
type Await struct {
	Selector string  `cborgen:"selector"`
	Link     cid.Cid `cborgen:"link"`
}
