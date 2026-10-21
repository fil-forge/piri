package policy_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/config/dynamic"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp/policy"
)

func TestNew_ZeroConfigMeansNoDelay(t *testing.T) {
	registry := dynamic.NewRegistry(nil)

	p, err := policy.New(registry, app.CommpConfig{})
	require.NoError(t, err)
	assert.Zero(t, p.Delay(), "a zero config is no delay, not the default")
}

func TestNew_HonorsConfiguredValue(t *testing.T) {
	registry := dynamic.NewRegistry(nil)

	p, err := policy.New(registry, app.CommpConfig{Delay: 20 * time.Minute})
	require.NoError(t, err)
	assert.Equal(t, 20*time.Minute, p.Delay())
}

func TestPolicy_ReadsThrough(t *testing.T) {
	registry := dynamic.NewRegistry(nil)

	p, err := policy.New(registry, app.CommpConfig{Delay: commp.DefaultDelay})
	require.NoError(t, err)
	require.Equal(t, commp.DefaultDelay, p.Delay())

	require.NoError(t, registry.Update(
		map[string]any{string(config.CommPDelay): "0s"},
		false, dynamic.SourceAPI,
	))
	assert.Zero(t, p.Delay(), "setting the delay to zero must apply to the running task")

	require.NoError(t, registry.Update(
		map[string]any{string(config.CommPDelay): "90m"},
		false, dynamic.SourceAPI,
	))
	assert.Equal(t, 90*time.Minute, p.Delay())
}

func TestPolicy_RejectsInvalidUpdates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   any
		wantErr string
	}{
		{"negative", "-1h", "outside valid range"},
		{"above the ceiling", "744h", "outside valid range"},
		{"not a duration", "soon", "invalid duration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := dynamic.NewRegistry(nil)
			p, err := policy.New(registry, app.CommpConfig{Delay: commp.DefaultDelay})
			require.NoError(t, err)

			err = registry.Update(
				map[string]any{string(config.CommPDelay): tc.value},
				false, dynamic.SourceAPI,
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Equal(t, commp.DefaultDelay, p.Delay(),
				"a rejected update must leave the delay untouched")
		})
	}
}
