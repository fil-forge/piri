package consolidation

import (
	"testing"

	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ipld/datamodel"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/stretchr/testify/require"
)

func TestConsolidation(t *testing.T) {
	t.Run("codec roundtrip", func(t *testing.T) {
		c := createTestConsolidation(t)
		codec := Codec{}

		encoded, err := codec.Encode(c)
		require.NoError(t, err)
		require.NotEmpty(t, encoded)

		decoded, err := codec.Decode(encoded)
		require.NoError(t, err)

		requireEqualConsolidation(t, c, decoded)
	})

	t.Run("track accessor round-trips the invocation", func(t *testing.T) {
		c := createTestConsolidation(t)
		codec := Codec{}

		encoded, err := codec.Encode(c)
		require.NoError(t, err)

		decoded, err := codec.Decode(encoded)
		require.NoError(t, err)

		original, err := c.Track()
		require.NoError(t, err)
		restored, err := decoded.Track()
		require.NoError(t, err)

		require.Equal(t, original.Link(), restored.Link())
	})
}

func createTestConsolidation(t *testing.T) Consolidation {
	t.Helper()

	signer := testutil.RandomSigner(t)
	audience := testutil.RandomDID(t)

	track, err := invocation.Invoke(
		signer,
		signer.DID(),
		"/space/egress/track",
		datamodel.Map{},
		invocation.WithAudience(audience),
	)
	require.NoError(t, err)

	return New(track, testutil.RandomCID(t))
}

func requireEqualConsolidation(t *testing.T, expected, actual Consolidation) {
	t.Helper()

	// Compare invocation envelope bytes (the canonical wire form).
	require.Equal(t, expected.TrackInvocationBytes, actual.TrackInvocationBytes)
	require.Equal(t, expected.ConsolidateInvocationCID, actual.ConsolidateInvocationCID)
}
