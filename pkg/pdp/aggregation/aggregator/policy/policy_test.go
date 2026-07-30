package policy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/config/dynamic"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/aggregator"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/aggregator/policy"
)

func TestNew_ZeroConfigUsesDefault(t *testing.T) {
	registry := dynamic.NewRegistry(nil)

	p, err := policy.New(registry, app.AggregatorConfig{})
	require.NoError(t, err)
	assert.Equal(t, aggregator.DefaultMinAggregateSize, p.MinAggregateSize())
}

func TestNew_HonorsConfiguredValue(t *testing.T) {
	registry := dynamic.NewRegistry(nil)

	p, err := policy.New(registry, app.AggregatorConfig{MinAggregateSize: 1 << 26})
	require.NoError(t, err)
	assert.Equal(t, uint64(1<<26), p.MinAggregateSize())
}

func TestPolicy_ReadsThrough(t *testing.T) {
	registry := dynamic.NewRegistry(nil)

	p, err := policy.New(registry, app.AggregatorConfig{})
	require.NoError(t, err)
	require.Equal(t, aggregator.DefaultMinAggregateSize, p.MinAggregateSize())

	require.NoError(t, registry.Update(
		map[string]any{string(config.AggregatorMinAggregateSize): 1 << 29},
		false, dynamic.SourceAPI,
	))

	assert.Equal(t, uint64(1<<29), p.MinAggregateSize(),
		"retuning the threshold must apply without rebuilding the fold task")
}

func TestPolicy_RejectsInvalidUpdates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   any
		wantErr string
	}{
		// The fold compares against padded sizes, which are powers of two, so
		// a threshold in between behaves identically to the lower one.
		{"not a power of two", 3 << 26, "power of two"},
		{"below the floor", 1 << 19, "outside valid range"},
		// Above this the worst-case aggregate (twice the threshold) exceeds
		// what zerocomm can zero-pad.
		{"above the structural ceiling", uint64(1) << 41, "outside valid range"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := dynamic.NewRegistry(nil)
			p, err := policy.New(registry, app.AggregatorConfig{})
			require.NoError(t, err)

			err = registry.Update(
				map[string]any{string(config.AggregatorMinAggregateSize): tc.value},
				false, dynamic.SourceAPI,
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Equal(t, aggregator.DefaultMinAggregateSize, p.MinAggregateSize(),
				"a rejected update must leave the threshold untouched")
		})
	}
}

// TestPolicy_IndependentOfPieceSize documents the decision that these are two
// unrelated knobs: the aggregation threshold is a gas/latency tradeoff, while
// the piece size limit is a memory-safety bound on the per-sub-piece memtree
// built at proving time. Nothing derives one from the other.
func TestPolicy_IndependentOfPieceSize(t *testing.T) {
	registry := dynamic.NewRegistry(nil)

	p, err := policy.New(registry, app.AggregatorConfig{})
	require.NoError(t, err)

	// A threshold far below the max piece size is legal: large pieces simply
	// each become their own aggregate.
	require.NoError(t, registry.Update(
		map[string]any{string(config.AggregatorMinAggregateSize): 1 << 20},
		false, dynamic.SourceAPI,
	))
	assert.Equal(t, uint64(1<<20), p.MinAggregateSize())

	// And far above it, which just means more pieces per aggregate.
	require.NoError(t, registry.Update(
		map[string]any{string(config.AggregatorMinAggregateSize): 1 << 33},
		false, dynamic.SourceAPI,
	))
	assert.Equal(t, uint64(1<<33), p.MinAggregateSize())
}
