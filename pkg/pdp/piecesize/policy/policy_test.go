package policy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/config/dynamic"
	"github.com/fil-forge/piri/pkg/pdp/piecesize"
	"github.com/fil-forge/piri/pkg/pdp/piecesize/policy"
)

func TestNew_ZeroConfigUsesDefaults(t *testing.T) {
	// testutil.NewTestConfig leaves PDPService entirely zero-valued, so every
	// fx-backed suite reaches this constructor with an empty config. It must
	// still produce a working policy rather than an error.
	registry := dynamic.NewRegistry(nil)

	p, err := policy.New(registry, app.PieceConfig{})
	require.NoError(t, err)

	assert.Equal(t, piecesize.DefaultMaxPaddedSize, p.Limits().MaxPadded())
	assert.Equal(t, uint64(266338304), p.MaxRaw())
}

func TestNew_HonorsConfiguredValue(t *testing.T) {
	registry := dynamic.NewRegistry(nil)

	p, err := policy.New(registry, app.PieceConfig{MaxPaddedSize: 1 << 29})
	require.NoError(t, err)

	assert.Equal(t, uint64(1<<29), p.Limits().MaxPadded())
	assert.Equal(t, uint64(532676608), p.MaxRaw())
}

func TestNew_RejectsInvalidConfiguredValue(t *testing.T) {
	registry := dynamic.NewRegistry(nil)

	_, err := policy.New(registry, app.PieceConfig{MaxPaddedSize: 3 << 27})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "power of two")
}

// TestPolicy_ReadsThrough is the point of the whole indirection: a runtime
// config change must be observed by an already-constructed Policy, without a
// restart and without rebuilding the consumers holding it.
func TestPolicy_ReadsThrough(t *testing.T) {
	registry := dynamic.NewRegistry(nil)

	p, err := policy.New(registry, app.PieceConfig{})
	require.NoError(t, err)
	require.Error(t, p.CheckRaw(300_000_000), "300 MB exceeds the 256 MiB default")

	require.NoError(t, registry.Update(
		map[string]any{string(config.PieceMaxPaddedSize): 1 << 29},
		false, dynamic.SourceAPI,
	))

	assert.Equal(t, uint64(1<<29), p.Limits().MaxPadded())
	assert.NoError(t, p.CheckRaw(300_000_000), "raised limit must apply to the same Policy")
}

// TestPolicy_RegistryRejectsUnprovableValues covers the reason the knob is
// schema-bounded: a limit Curio cannot prove would not fail at ingest, it
// would fail at proving time as a fault.
func TestPolicy_RegistryRejectsUnprovableValues(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   any
		wantErr string
	}{
		{"above curio ceiling", 1 << 31, "outside valid range"},
		{"not a power of two", 3 << 27, "power of two"},
		{"below minimum", 1 << 27, "outside valid range"},
		{"zero", 0, "power of two"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := dynamic.NewRegistry(nil)
			p, err := policy.New(registry, app.PieceConfig{})
			require.NoError(t, err)

			err = registry.Update(
				map[string]any{string(config.PieceMaxPaddedSize): tc.value},
				false, dynamic.SourceAPI,
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)

			assert.Equal(t, piecesize.DefaultMaxPaddedSize, p.Limits().MaxPadded(),
				"a rejected update must leave the limit untouched")
		})
	}

	t.Run("curio ceiling itself is accepted", func(t *testing.T) {
		registry := dynamic.NewRegistry(nil)
		p, err := policy.New(registry, app.PieceConfig{})
		require.NoError(t, err)

		require.NoError(t, registry.Update(
			map[string]any{string(config.PieceMaxPaddedSize): int(piecesize.CurioMaxPaddedSize)},
			false, dynamic.SourceAPI,
		))
		assert.Equal(t, piecesize.CurioMaxPaddedSize, p.Limits().MaxPadded())
	})
}

// TestPolicy_AcceptsStringFromCLI covers `piri client admin config set`, which
// deliberately sends values as raw strings and relies on the schema to parse.
func TestPolicy_AcceptsStringFromCLI(t *testing.T) {
	registry := dynamic.NewRegistry(nil)
	p, err := policy.New(registry, app.PieceConfig{})
	require.NoError(t, err)

	require.NoError(t, registry.Update(
		map[string]any{string(config.PieceMaxPaddedSize): "536870912"},
		false, dynamic.SourceAPI,
	))
	assert.Equal(t, uint64(1<<29), p.Limits().MaxPadded())
}
