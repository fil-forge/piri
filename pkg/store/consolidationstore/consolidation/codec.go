//go:build !codegen

package consolidation

import (
	"bytes"
	"fmt"

	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/ipfs/go-cid"
)

// New constructs a Consolidation by capturing the bytes of `track` and
// recording the consolidate-invocation CID promised by the egress
// tracker. Callers pass the typed invocation; the bytes form is what
// gets persisted.
func New(track ucan.Invocation, consolidate cid.Cid) Consolidation {
	return Consolidation{
		TrackInvocationBytes:     track.Bytes(),
		ConsolidateInvocationCID: consolidate,
	}
}

// Track decodes the persisted track invocation envelope into a typed
// *invocation.Invocation. Callers do this on demand because the typed
// value isn't held in the struct (see Consolidation doc).
func (c Consolidation) Track() (*invocation.Invocation, error) {
	if len(c.TrackInvocationBytes) == 0 {
		return nil, fmt.Errorf("consolidation has no track invocation bytes")
	}
	inv, err := invocation.Decode(c.TrackInvocationBytes)
	if err != nil {
		return nil, fmt.Errorf("decoding track invocation: %w", err)
	}
	return inv, nil
}

// Encode produces a self-contained CBOR envelope for the consolidation
// via the cborgen-generated MarshalCBOR method.
func Encode(c Consolidation) ([]byte, error) {
	var buf bytes.Buffer
	if err := c.MarshalCBOR(&buf); err != nil {
		return nil, fmt.Errorf("encoding consolidation: %w", err)
	}
	return buf.Bytes(), nil
}

// Decode parses a CBOR envelope produced by Encode.
func Decode(data []byte) (Consolidation, error) {
	var c Consolidation
	if err := c.UnmarshalCBOR(bytes.NewReader(data)); err != nil {
		return Consolidation{}, fmt.Errorf("decoding consolidation: %w", err)
	}
	return c, nil
}

// Codec implements genericstore.Codec for Consolidation values.
type Codec struct{}

func (Codec) Encode(c Consolidation) ([]byte, error) { return Encode(c) }

func (Codec) Decode(data []byte) (Consolidation, error) { return Decode(data) }
