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
	PDPAccept promise.AwaitOK `cborgen:"pdpAccept"`
	// ExecutedAt is the approximate time (in seconds since unix epoch) that
	// the `/blob/accept` invocation was executed.
	ExecutedAt uint64 `cborgen:"executedAt"`
	// Cause is a link to the `/blob/accept` task that requested the
	// acceptance.
	Cause cid.Cid `cborgen:"cause"`
	// Site is a link to the location commitment minted at acceptance —
	// the digest→claim index `/blob/release` uses to delete the location
	// claim when this space's acceptance is removed.
	Site cid.Cid `cborgen:"site"`
}

// Blob captures the bytes the acceptance attests to.
type Blob struct {
	Digest multihash.Multihash `cborgen:"digest"`
	Size   uint64              `cborgen:"size"`
}
