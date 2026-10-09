package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config"
)

// Deployment scripts compare this output with the config_version of the config
// on disk, so it must be the bare number. A config that fails to load must not
// stop it: version does not read config.
func TestVersionConfigVersion(t *testing.T) {
	malformed := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(malformed, []byte("this is not toml = = ="), 0o600))

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"--config", malformed, "version", "--config-version"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
		cfgFile = ""
		_ = versionCmd.Flags().Set("config-version", "false")
	})

	require.NoError(t, rootCmd.Execute())
	require.Equal(t, fmt.Sprintf("%d\n", config.GeneratedConfigVersion), out.String())
}
