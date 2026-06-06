package consolidation_test

import (
	"testing"

	"github.com/fil-forge/libforge/commands"
	"github.com/fil-forge/libforge/commands/space/egress"
	"github.com/fil-forge/ucantone/testutil"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store/consolidationstore/consolidation"
)

func TestConsolidation(t *testing.T) {
	t.Run("encode/decode roundtrip", func(t *testing.T) {
		c := createTestConsolidation(t)

		encoded, err := consolidation.Encode(c)
		require.NoError(t, err)
		require.NotEmpty(t, encoded)

		decoded, err := consolidation.Decode(encoded)
		require.NoError(t, err)

		requireEqualConsolidation(t, c, decoded)
	})

	t.Run("codec roundtrip", func(t *testing.T) {
		c := createTestConsolidation(t)
		codec := consolidation.Codec{}

		encoded, err := codec.Encode(c)
		require.NoError(t, err)

		decoded, err := codec.Decode(encoded)
		require.NoError(t, err)

		requireEqualConsolidation(t, c, decoded)
	})

	t.Run("Track accessor decodes the persisted envelope", func(t *testing.T) {
		c := createTestConsolidation(t)
		inv, err := c.Track()
		require.NoError(t, err)
		require.NotNil(t, inv)
		require.Equal(t, egress.Track.Command, inv.Command())
	})

	t.Run("Track on empty bytes errors", func(t *testing.T) {
		_, err := consolidation.Consolidation{}.Track()
		require.Error(t, err)
	})
}

func createTestConsolidation(t *testing.T) consolidation.Consolidation {
	t.Helper()

	signer := testutil.RandomIssuer(t)
	audience := testutil.RandomDID(t)

	inv, err := egress.Track.Invoke(
		signer,
		audience,
		&egress.TrackArguments{
			Receipts: testutil.RandomCID(t),
			Endpoint: commands.CborURL{},
		},
	)
	require.NoError(t, err)

	return consolidation.New(inv, testutil.RandomCID(t))
}

func requireEqualConsolidation(t *testing.T, expected, actual consolidation.Consolidation) {
	t.Helper()

	// The persisted bytes round-trip exactly.
	require.Equal(t, expected.TrackInvocationBytes, actual.TrackInvocationBytes)
	require.Equal(t, expected.ConsolidateInvocationCID, actual.ConsolidateInvocationCID)

	// And the decoded invocation links match.
	expectedInv, err := expected.Track()
	require.NoError(t, err)
	actualInv, err := actual.Track()
	require.NoError(t, err)
	require.Equal(t, expectedInv.Link(), actualInv.Link())
}
