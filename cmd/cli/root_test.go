package cli

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config"
)

func TestBlobDirFromEnv(t *testing.T) {
	t.Setenv("PIRI_REPO_BLOB_DIR", "/mnt/blobs")

	var cfg config.FullServerConfig
	require.NoError(t, viper.Unmarshal(&cfg))

	assert.Equal(t, "/mnt/blobs", cfg.Repo.BlobDir)
}

func TestBlobDirFromFlag(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("blob-dir")
	require.NoError(t, flag.Value.Set("/mnt/blobs"))
	t.Cleanup(func() { _ = flag.Value.Set("") })

	var cfg config.FullServerConfig
	require.NoError(t, viper.Unmarshal(&cfg))

	assert.Equal(t, "/mnt/blobs", cfg.Repo.BlobDir)
}
