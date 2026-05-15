package consolidation

import (
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/ipfs/go-cid"
)

// Consolidation holds the track invocation and consolidate invocation CID for
// a batch of egress receipts. The track invocation is stored as its raw
// dag-cbor envelope bytes; use Track() to decode back to the typed value.
type Consolidation struct {
	// TrackInvocationBytes is the raw dag-cbor envelope of the
	// `/space/egress/track` invocation that was sent to the egress tracker.
	TrackInvocationBytes []byte `cborgen:"trackInvocation"`
	// ConsolidateInvocationCID is the CID of the `/space/egress/consolidate`
	// invocation from the receipt's fork effect.
	ConsolidateInvocationCID cid.Cid `cborgen:"consolidateInvocationCID"`
}

// New builds a Consolidation by capturing the raw envelope bytes of the track
// invocation alongside the consolidate invocation CID.
func New(track *invocation.Invocation, consolidate cid.Cid) Consolidation {
	return Consolidation{
		TrackInvocationBytes:     track.Bytes(),
		ConsolidateInvocationCID: consolidate,
	}
}

// Track decodes the stored track invocation back to a typed Invocation.
func (c Consolidation) Track() (*invocation.Invocation, error) {
	return invocation.Decode(c.TrackInvocationBytes)
}
