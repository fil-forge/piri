// Package policy binds the aggregation threshold to the dynamic config
// registry, so an operator can retune it at runtime without a restart.
//
// It is separate from the aggregator package for the same reason
// pkg/pdp/piecesize/policy is separate from pkg/pdp/piecesize: pkg/config
// needs the default value, and pkg/config/dynamic imports pkg/config, so the
// registry glue cannot live alongside the constant without forming a cycle.
package policy

import (
	"fmt"

	"github.com/fil-forge/piri/pkg/config"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/config/dynamic"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/aggregator"
)

// New registers the aggregation threshold with the dynamic registry and
// returns a Policy reading through it.
//
// A zero cfg.MinAggregateSize means the default rather than an error, so a
// zero-valued config (as every fx-backed test uses) still works.
func New(registry *dynamic.Registry, cfg app.AggregatorConfig) (aggregator.Policy, error) {
	fallback := cfg.MinAggregateSize
	if fallback == 0 {
		fallback = aggregator.DefaultMinAggregateSize
	}

	if err := registry.RegisterEntries(map[config.Key]dynamic.ConfigEntry{
		config.AggregatorMinAggregateSize: {
			Value: uint(fallback),
			// Powers of two only: the fold compares against padded sizes,
			// which are themselves powers of two, so a threshold between two
			// of them behaves identically to the lower one.
			Schema: dynamic.PowerOfTwoSchema{
				Min: uint(aggregator.MinAllowedAggregateSize),
				Max: uint(aggregator.MaxAllowedAggregateSize),
			},
		},
	}); err != nil {
		return aggregator.Policy{}, fmt.Errorf("registering aggregator config entries: %w", err)
	}

	return aggregator.NewPolicy(func() uint64 {
		return uint64(registry.GetUint(config.AggregatorMinAggregateSize, uint(fallback)))
	}), nil
}
