package config

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config/app"
)

func TestPostgresConfig_ToAppConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := PostgresConfig{
			URL:             "postgres://user:pass@localhost:5432/db?sslmode=disable",
			MaxOpenConns:    10,
			MaxIdleConns:    5,
			ConnMaxLifetime: "15m",
		}

		result, err := cfg.ToAppConfig()
		require.NoError(t, err)

		assert.Equal(t, "localhost:5432", result.URL.Host)
		assert.Equal(t, "/db", result.URL.Path)
		assert.Equal(t, 10, result.MaxOpenConns)
		assert.Equal(t, 5, result.MaxIdleConns)
		assert.Equal(t, 15*time.Minute, result.ConnMaxLifetime)
	})

	t.Run("empty URL returns error", func(t *testing.T) {
		cfg := PostgresConfig{}
		_, err := cfg.ToAppConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "URL is required")
	})

	t.Run("invalid URL returns error", func(t *testing.T) {
		cfg := PostgresConfig{URL: "://invalid"}
		_, err := cfg.ToAppConfig()
		assert.Error(t, err)
	})

	t.Run("invalid duration returns error", func(t *testing.T) {
		cfg := PostgresConfig{
			URL:             "postgres://localhost/db",
			ConnMaxLifetime: "invalid",
		}
		_, err := cfg.ToAppConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conn_max_lifetime")
	})

	t.Run("zero values use defaults", func(t *testing.T) {
		cfg := PostgresConfig{URL: "postgres://localhost/db"}
		result, err := cfg.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, 0, result.MaxOpenConns) // 0 means use default
		assert.Equal(t, time.Duration(0), result.ConnMaxLifetime)
	})
}

func TestDatabaseConfig_ToAppConfig(t *testing.T) {
	t.Run("sqlite type", func(t *testing.T) {
		cfg := DatabaseConfig{Type: "sqlite"}
		result, err := cfg.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, app.DatabaseTypeSQLite, result.Type)
	})

	t.Run("empty type defaults to sqlite", func(t *testing.T) {
		cfg := DatabaseConfig{}
		result, err := cfg.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, app.DatabaseTypeSQLite, result.Type)
	})

	t.Run("postgres type", func(t *testing.T) {
		cfg := DatabaseConfig{
			Type: "postgres",
			Postgres: PostgresConfig{
				URL: "postgres://localhost/db",
			},
		}
		result, err := cfg.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, app.DatabaseTypePostgres, result.Type)
		assert.Equal(t, "/db", result.Postgres.URL.Path)
	})

	t.Run("postgres without URL returns error", func(t *testing.T) {
		cfg := DatabaseConfig{Type: "postgres"}
		_, err := cfg.ToAppConfig()
		assert.Error(t, err)
	})
}

func TestRepoConfig_ToAppConfig_EgressJournalDir(t *testing.T) {
	dataDir := t.TempDir()
	journalDir := filepath.Join(t.TempDir(), "journal")

	testCases := []struct {
		desc             string
		egressJournalDir string
		expected         app.EgressTrackerStorageConfig
	}{
		{
			desc:     "unset defaults to the data dir",
			expected: app.EgressTrackerStorageConfig{Dir: filepath.Join(dataDir, "egress_tracker", "journal")},
		},
		{
			desc:             "set overrides the data dir",
			egressJournalDir: journalDir,
			expected:         app.EgressTrackerStorageConfig{Dir: journalDir},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			cfg := RepoConfig{
				DataDir:          dataDir,
				TempDir:          t.TempDir(),
				EgressJournalDir: tc.egressJournalDir,
			}
			result, err := cfg.ToAppConfig()
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result.EgressTracker)
		})
	}
}
