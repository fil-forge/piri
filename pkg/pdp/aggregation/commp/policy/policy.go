// Package policy binds the commP delay to the dynamic config registry, so an
// operator can retune it at runtime without a restart. It is separate from
// the commp package for the reason pkg/pdp/aggregation/aggregator/policy is
// separate from its package: pkg/config needs the default value, and
// pkg/config/dynamic imports pkg/config, so the registry glue cannot live
// alongside the constant without forming a cycle.
package policy

import (
	"fmt"
	"time"

	"github.com/fil-forge/piri/pkg/config"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/config/dynamic"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
)

// New registers the commP delay with the dynamic registry and returns a
// Policy reading through it. cfg.Delay is the value until an operator changes
// it; zero means no delay, as it does on the key.
func New(registry *dynamic.Registry, cfg app.CommpConfig) (commp.Policy, error) {
	fallback := cfg.Delay
	if err := registry.RegisterEntries(map[config.Key]dynamic.ConfigEntry{
		config.CommPDelay: {
			Value:  fallback,
			Schema: dynamic.DurationSchema{Min: 0, Max: commp.MaxDelay},
		},
	}); err != nil {
		return commp.Policy{}, fmt.Errorf("registering commp config entries: %w", err)
	}

	return commp.NewPolicy(func() time.Duration {
		return registry.GetDuration(config.CommPDelay, fallback)
	}), nil
}
