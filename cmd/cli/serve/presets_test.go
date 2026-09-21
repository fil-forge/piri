package serve

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config"
	"github.com/fil-forge/piri/pkg/presets"
)

// A network preset fills in the indexer keys, so an operator disables the
// integration under a preset by setting both to empty strings, as
// docs/content/configuration/ucan.md says. This pins that viper takes the
// explicit empty values over the preset defaults.
func TestEmptyIndexerKeysOverridePreset(t *testing.T) {
	viper.Set("network", string(presets.ForgeProd))
	require.NoError(t, loadPresets())
	viper.SetConfigType("toml")
	require.NoError(t, viper.ReadConfig(strings.NewReader(`
[ucan.services.indexer]
did = ""
url = ""
`)))

	var cfg config.FullServerConfig
	require.NoError(t, viper.Unmarshal(&cfg))

	require.Equal(t, config.IndexingServiceConfig{}, cfg.UCANService.Services.Indexer)
}
