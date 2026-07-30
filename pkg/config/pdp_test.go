package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/pdp/piecesize"
)

func TestPieceConfig_ToAppConfig(t *testing.T) {
	t.Run("zero means default", func(t *testing.T) {
		// An omitted [pdp.piece] section must not be an error.
		got, err := PieceConfig{}.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, piecesize.DefaultMaxPaddedSize, got.MaxPaddedSize)
	})

	t.Run("accepts a valid power of two", func(t *testing.T) {
		got, err := PieceConfig{MaxPaddedSize: 1 << 29}.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, uint64(1<<29), got.MaxPaddedSize)
	})

	t.Run("accepts the curio ceiling", func(t *testing.T) {
		got, err := PieceConfig{MaxPaddedSize: piecesize.CurioMaxPaddedSize}.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, piecesize.CurioMaxPaddedSize, got.MaxPaddedSize)
	})

	for _, tc := range []struct {
		name    string
		size    uint64
		wantErr string
	}{
		{"above the curio ceiling", 1 << 31, "exceeds the proving limit"},
		{"not a power of two", 3 << 27, "power of two"},
		{"below the minimum", 1 << 19, "below the minimum"},
	} {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			_, err := PieceConfig{MaxPaddedSize: tc.size}.ToAppConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "pdp.piece.max_padded_size")
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
