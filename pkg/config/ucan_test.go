package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePLCDirectory(t *testing.T) {
	t.Run("empty string falls back to the default", func(t *testing.T) {
		u, err := parsePLCDirectory("")
		require.NoError(t, err)
		require.NotNil(t, u)
		assert.Equal(t, DefaultPLCDirectory, u.String())
	})

	t.Run("valid URL yields non-nil", func(t *testing.T) {
		u, err := parsePLCDirectory("https://plc.directory")
		require.NoError(t, err)
		require.NotNil(t, u)
		assert.Equal(t, "https", u.Scheme)
		assert.Equal(t, "plc.directory", u.Host)
	})

	t.Run("invalid URL returns descriptive error", func(t *testing.T) {
		u, err := parsePLCDirectory("://invalid")
		require.Error(t, err)
		assert.Nil(t, u)
		assert.Contains(t, err.Error(), "PLC directory URL")
	})
}
