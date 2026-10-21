package serve

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config"
)

// The lotus auth token has no CLI flag, so viper only sees PIRI_PDP_LOTUS_AUTH_TOKEN
// through the BindEnv call in this package's init.
func TestLotusAuthTokenFromEnv(t *testing.T) {
	t.Setenv("PIRI_PDP_LOTUS_AUTH_TOKEN", "test-token")

	var cfg config.FullServerConfig
	require.NoError(t, viper.Unmarshal(&cfg))

	assert.Equal(t, "test-token", cfg.PDPService.LotusAuthToken)
}

// telemetry.environment has no CLI flag either. viper.AutomaticEnv alone does
// not make an otherwise-unregistered key visible to Unmarshal, so without the
// BindEnv call in this package's init the value would be dropped and Setup
// would fall back to the network, or to "custom".
func TestTelemetryEnvironmentFromEnv(t *testing.T) {
	t.Setenv("PIRI_TELEMETRY_ENVIRONMENT", "staging")

	var cfg config.FullServerConfig
	require.NoError(t, viper.Unmarshal(&cfg))

	assert.Equal(t, "staging", cfg.Telemetry.Environment)
}
