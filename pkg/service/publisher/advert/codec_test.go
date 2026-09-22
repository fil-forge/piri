package advert

import (
	"testing"

	"github.com/fil-forge/libforge/testutil"
	"github.com/stretchr/testify/require"
)

// TestSpecRoundTrip pins that a spec survives the row: what Publish encodes
// is what the publish task decodes.
func TestSpecRoundTrip(t *testing.T) {
	in := Spec{
		ContextID: []byte("context"),
		Digest:    testutil.RandomMultihash(t),
		Metadata:  []byte{0xa1, 0x01, 0x02},
	}
	b, err := Encode(in)
	require.NoError(t, err)
	out, err := Decode(b)
	require.NoError(t, err)
	require.Equal(t, in, out)

	_, err = Decode([]byte("not cbor"))
	require.Error(t, err, "a row holding something other than a spec must not pass as one")
}
