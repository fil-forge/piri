package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config"
)

// Deployment scripts compare this output with the config_version of the config
// on disk, so it must be the bare number. A config that fails to load must not
// stop it: version does not read config.
func TestVersionConfigVersion(t *testing.T) {
	malformed := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(malformed, []byte("this is not toml = = ="), 0o600))

	for _, args := range [][]string{
		{"--config", malformed, "version", "--config-version"},
		{"--config", malformed, "version"},
	} {
		var out bytes.Buffer
		execute(t, &out, args...)
		if args[len(args)-1] == "--config-version" {
			require.Equal(t, fmt.Sprintf("%d\n", config.GeneratedConfigVersion), out.String())
		} else {
			require.Contains(t, out.String(), "version: ")
		}
	}
}

// Skipping config for version must not outlast that run: another command run
// later in the same process still loads its config.
func TestConfigLoadsAfterVersion(t *testing.T) {
	// Viper is global, so the value is unique to this run.
	cfg := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(cfg, fmt.Appendf(nil, "[probe]\nloaded = %q\n", cfg), 0o600))

	probe := &cobra.Command{Use: "probe", Run: func(*cobra.Command, []string) {}}
	rootCmd.AddCommand(probe)
	t.Cleanup(func() { rootCmd.RemoveCommand(probe) })

	execute(t, &bytes.Buffer{}, "--config", cfg, "version", "--config-version")
	require.NotEqual(t, cfg, viper.GetString("probe.loaded"), "version loaded the config")

	execute(t, &bytes.Buffer{}, "--config", cfg, "probe")
	require.Equal(t, cfg, viper.GetString("probe.loaded"))
}

// execute runs the root command with args, writing its output to out. Flag
// values outlive a run, so the ones these tests set are reset around it.
func execute(t *testing.T, out *bytes.Buffer, args ...string) {
	t.Helper()
	reset := func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
		cfgFile = ""
		_ = versionCmd.Flags().Set("config-version", "false")
	}
	reset()
	t.Cleanup(reset)
	rootCmd.SetOut(out)
	rootCmd.SetArgs(args)
	require.NoError(t, rootCmd.Execute())
}
