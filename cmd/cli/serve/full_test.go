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
