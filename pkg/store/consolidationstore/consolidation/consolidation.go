package consolidation

import (
	"github.com/ipfs/go-cid"
)

// Consolidation pairs a /space/egress/track invocation with the CID of
// the /space/egress/consolidate invocation that the egress tracker
// promised as a forked effect.
//
// The track invocation is stored as a raw envelope (CBOR bytes from
// invocation.Bytes) rather than as a *invocation.Invocation because
// ucantone invocations are multi-field envelopes that cborgen cannot
// inline cleanly as a field; persisting the bytes lets us round-trip
// through cborgen and reconstruct the typed value on demand via the
// Track accessor.
type Consolidation struct {
	// TrackInvocationBytes is the raw CBOR envelope of the
	// /space/egress/track invocation sent to the tracker service.
	TrackInvocationBytes []byte `cborgen:"trackInvocation"`
	// ConsolidateInvocationCID is the CID of the /space/egress/
	// consolidate invocation the tracker promised as a fork effect
	// on the track receipt.
	ConsolidateInvocationCID cid.Cid `cborgen:"consolidateInvocationCID"`
}
