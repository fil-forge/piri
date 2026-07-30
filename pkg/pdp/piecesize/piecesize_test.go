package piecesize_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/pdp/piecesize"
)

// TestCurioCeiling pins the ceiling to Curio's own constant. If a Curio bump
// changes MaxMemtreeSize this test does not fail — that is the point, the
// value tracks upstream — but the assertion documents what it is today and
// the RAM implication of changing it.
func TestCurioCeiling(t *testing.T) {
	assert.Equal(t, uint64(1<<30), piecesize.CurioMaxPaddedSize,
		"curio lib/proof.MaxMemtreeSize; peak prove RSS is ~3x this")
	assert.LessOrEqual(t, piecesize.DefaultMaxPaddedSize, piecesize.CurioMaxPaddedSize)
}

// TestPaddedForRaw covers the power-of-two rounding boundary that motivates
// configuring the limit in padded rather than raw terms.
func TestPaddedForRaw(t *testing.T) {
	for _, tc := range []struct {
		name       string
		raw        uint64
		wantPadded uint64
	}{
		{"exact 2^28 fit", 266338304, 1 << 28},
		{"one byte over spills to 2^29", 266338305, 1 << 29},
		{"libforge MaxBlobSize spills to 2^29", 268435456, 1 << 29},
		{"exact 2^29 fit", 532676608, 1 << 29},
		{"curio ceiling exact fit", 1065353216, 1 << 30},
		{"tiny piece floors at 128", 1, 128},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := piecesize.PaddedForRaw(tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPadded, got)
		})
	}
}

// TestMaxRawForPadded is the inverse, and asserts the round trip: the raw cap
// derived from a padded size must pad back to exactly that size.
func TestMaxRawForPadded(t *testing.T) {
	assert.Equal(t, uint64(266338304), piecesize.MaxRawForPadded(1<<28),
		"must match the pre-refactor PieceSizeLimit exactly")

	for shift := 21; shift <= 30; shift++ {
		padded := uint64(1) << shift
		t.Run(fmt.Sprintf("2^%d", shift), func(t *testing.T) {
			raw := piecesize.MaxRawForPadded(padded)

			back, err := piecesize.PaddedForRaw(raw)
			require.NoError(t, err)
			assert.Equal(t, padded, back, "max raw must pad to exactly the padded limit")

			over, err := piecesize.PaddedForRaw(raw + 1)
			require.NoError(t, err)
			assert.Equal(t, padded<<1, over, "one byte past the cap must spill a level")
		})
	}
}

func TestValidatePaddedSize(t *testing.T) {
	for _, tc := range []struct {
		name    string
		padded  uint64
		wantErr string
	}{
		{name: "default", padded: piecesize.DefaultMaxPaddedSize},
		{name: "minimum", padded: piecesize.MinPaddedSize},
		{name: "curio ceiling", padded: piecesize.CurioMaxPaddedSize},
		{name: "512 MiB", padded: 1 << 29},

		{name: "zero", padded: 0, wantErr: "power of two"},
		{name: "not a power of two", padded: 3 << 27, wantErr: "power of two"},
		{name: "one over the ceiling", padded: (1 << 30) + 1, wantErr: "power of two"},
		{name: "double the ceiling", padded: 1 << 31, wantErr: "exceeds the proving limit"},
		{name: "below the minimum", padded: 1 << 19, wantErr: "below the minimum"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := piecesize.ValidatePaddedSize(tc.padded)
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestLimits(t *testing.T) {
	t.Run("zero value reports defaults", func(t *testing.T) {
		var l piecesize.Limits
		assert.Equal(t, piecesize.DefaultMaxPaddedSize, l.MaxPadded())
		assert.Equal(t, uint64(266338304), l.MaxRaw())
		assert.Equal(t, piecesize.DefaultLimits.MaxRaw(), l.MaxRaw())
	})

	t.Run("MaxUnpadded agrees with MaxRaw", func(t *testing.T) {
		for shift := 21; shift <= 30; shift++ {
			l := piecesize.Limits{Padded: 1 << shift}
			assert.Equal(t, abi.UnpaddedPieceSize(l.MaxRaw()), l.MaxUnpadded())
		}
	})

	t.Run("CheckRaw at the boundary", func(t *testing.T) {
		l := piecesize.Limits{Padded: 1 << 28}

		assert.NoError(t, l.CheckRaw(266338304), "the cap itself is allowed")

		err := l.CheckRaw(266338305)
		require.Error(t, err)

		var exceeded *piecesize.ExceededError
		require.True(t, errors.As(err, &exceeded))
		assert.Equal(t, uint64(266338305), exceeded.Size)
		assert.Equal(t, uint64(266338304), exceeded.MaxRaw)
		assert.Equal(t, uint64(1<<28), exceeded.MaxPadded)
	})
}

func TestPolicy(t *testing.T) {
	t.Run("zero value reports defaults", func(t *testing.T) {
		var p piecesize.Policy
		assert.Equal(t, piecesize.DefaultLimits, p.Limits())
		assert.Equal(t, uint64(266338304), p.MaxRaw())
		assert.NoError(t, p.CheckRaw(266338304))
		assert.Error(t, p.CheckRaw(266338305))
	})

	t.Run("reads through on every call", func(t *testing.T) {
		current := piecesize.Limits{Padded: 1 << 28}
		p := piecesize.NewPolicy(func() piecesize.Limits { return current })

		require.Error(t, p.CheckRaw(300_000_000))

		current = piecesize.Limits{Padded: 1 << 29}
		assert.NoError(t, p.CheckRaw(300_000_000),
			"a limit change must be observed without rebuilding the Policy")
	})
}
