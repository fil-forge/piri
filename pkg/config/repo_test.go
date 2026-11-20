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

func TestRepoConfig_ToAppConfig_BlobDir(t *testing.T) {
	type blobPaths struct {
		PDPStore  string
		Publisher string
	}

	dataDir := t.TempDir()
	blobDir := t.TempDir()

	testCases := []struct {
		desc     string
		blobDir  string
		expected blobPaths
	}{
		{
			desc: "unset keeps blobs in the data dir",
			expected: blobPaths{
				PDPStore:  filepath.Join(dataDir, "pdp", "datastore"),
				Publisher: filepath.Join(dataDir, "ipni"),
			},
		},
		{
			desc:    "set moves blobs to the blob dir",
			blobDir: blobDir,
			expected: blobPaths{
				PDPStore:  filepath.Join(blobDir, "pdp", "datastore"),
				Publisher: filepath.Join(blobDir, "ipni"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			cfg := RepoConfig{
				DataDir: dataDir,
				TempDir: t.TempDir(),
				BlobDir: tc.blobDir,
			}
			result, err := cfg.ToAppConfig()
			require.NoError(t, err)
			assert.Equal(t, tc.expected, blobPaths{
				PDPStore:  result.PDPStore.Dir,
				Publisher: result.Publisher.Dir,
			})
		})
	}
}

func TestS3Config_Validate(t *testing.T) {
	validConfigs := map[string]S3Config{
		"bucket_prefix":            {Endpoint: "objects.example.com", BucketPrefix: "piri-"},
		"path with a folder":       {Endpoint: "objects.example.com", Path: "fil-one/piri-0/"},
		"path with a bucket alone": {Endpoint: "objects.example.com", Path: "fil-one"},
	}
	for description, cfg := range validConfigs {
		t.Run("accepts "+description, func(t *testing.T) {
			require.NoError(t, cfg.Validate())
		})
	}

	invalidConfigs := []struct {
		description string
		cfg         S3Config
		message     string
	}{
		{
			description: "both bucket_prefix and path",
			cfg:         S3Config{Endpoint: "objects.example.com", BucketPrefix: "piri-", Path: "fil-one/"},
			message:     "mutually exclusive",
		},
		{
			description: "neither bucket_prefix nor path",
			cfg:         S3Config{Endpoint: "objects.example.com"},
			message:     "bucket_prefix or path is required",
		},
		{
			description: "a bucket_prefix with a slash",
			cfg:         S3Config{Endpoint: "objects.example.com", BucketPrefix: "fil-one/piri-0/"},
			message:     "set path to keep the objects in a folder",
		},
		{
			description: "a path with no bucket before its first slash",
			cfg:         S3Config{Endpoint: "objects.example.com", Path: "/piri-0/"},
			message:     "must start with a bucket name",
		},
		{
			description: "a path whose folder lacks a trailing slash",
			cfg:         S3Config{Endpoint: "objects.example.com", Path: "fil-one/piri-0"},
			message:     `must end with "/"`,
		},
	}
	for _, tc := range invalidConfigs {
		t.Run("rejects "+tc.description, func(t *testing.T) {
			require.ErrorContains(t, tc.cfg.Validate(), tc.message)
		})
	}
}
