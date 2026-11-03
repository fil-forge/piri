package cli

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config"
)

func TestEgressJournalDirFromEnv(t *testing.T) {
	t.Setenv("PIRI_REPO_EGRESS_JOURNAL_DIR", "/fast/egress-journal")

	var cfg config.FullServerConfig
	require.NoError(t, viper.Unmarshal(&cfg))

	assert.Equal(t, "/fast/egress-journal", cfg.Repo.EgressJournalDir)
}

func TestEgressJournalDirFromFlag(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("egress-journal-dir")
	require.NoError(t, flag.Value.Set("/fast/egress-journal"))
	t.Cleanup(func() { _ = flag.Value.Set("") })

	var cfg config.FullServerConfig
	require.NoError(t, viper.Unmarshal(&cfg))

	assert.Equal(t, "/fast/egress-journal", cfg.Repo.EgressJournalDir)
}
