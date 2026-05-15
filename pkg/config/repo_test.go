package config

import (
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

	t.Run("sqlite type with postgres section configured returns error", func(t *testing.T) {
		cfg := DatabaseConfig{
			Type: "sqlite",
			Postgres: PostgresConfig{
				URL: "postgres://localhost/db",
			},
		}
		_, err := cfg.ToAppConfig()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "one backend")
	})

	t.Run("empty type with postgres section configured returns error", func(t *testing.T) {
		cfg := DatabaseConfig{
			Postgres: PostgresConfig{
				URL: "postgres://localhost/db",
			},
		}
		_, err := cfg.ToAppConfig()
		require.Error(t, err)
	})
}

func TestObjectStoreConfig_ToAppConfig(t *testing.T) {
	t.Run("memory type with empty data_dir", func(t *testing.T) {
		r := RepoConfig{
			DataDir: "",
			ObjectStore: ObjectStoreConfig{Type: "memory"},
		}
		result, err := r.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, app.ObjectStoreTypeMemory, result.ObjectStore.Type)
		assert.Equal(t, app.S3Config{}, result.ObjectStore.S3)
		assert.Equal(t, app.LocalStorePaths{}, result.ObjectStore.Local)
	})

	t.Run("empty type with empty data_dir defaults to memory", func(t *testing.T) {
		r := RepoConfig{DataDir: ""}
		result, err := r.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, app.ObjectStoreTypeMemory, result.ObjectStore.Type)
	})

	t.Run("filesystem type populates Local and Filesystem paths", func(t *testing.T) {
		r := RepoConfig{
			DataDir:     t.TempDir(),
			TempDir:     t.TempDir(),
			ObjectStore: ObjectStoreConfig{Type: "filesystem"},
		}
		result, err := r.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, app.ObjectStoreTypeFilesystem, result.ObjectStore.Type)
		assert.NotEmpty(t, result.ObjectStore.Local.KeyStore.Dir)
		assert.NotEmpty(t, result.ObjectStore.Local.RetrievalJournal.Dir)
		assert.NotEmpty(t, result.ObjectStore.Filesystem.Allocations.Dir)
		assert.NotEmpty(t, result.ObjectStore.Filesystem.PDP.Dir)
		assert.Equal(t, app.S3Config{}, result.ObjectStore.S3)
	})

	t.Run("s3 type populates Local + S3, leaves Filesystem empty", func(t *testing.T) {
		r := RepoConfig{
			DataDir: t.TempDir(),
			TempDir: t.TempDir(),
			ObjectStore: ObjectStoreConfig{
				Type: "s3",
				S3: &S3Config{
					Endpoint:     "minio.example.com:9000",
					BucketPrefix: "piri-",
				},
			},
		}
		result, err := r.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, app.ObjectStoreTypeS3, result.ObjectStore.Type)
		assert.Equal(t, "minio.example.com:9000", result.ObjectStore.S3.Endpoint)
		assert.NotEmpty(t, result.ObjectStore.Local.KeyStore.Dir)
		assert.Equal(t, app.FilesystemBulkPaths{}, result.ObjectStore.Filesystem)
	})

	t.Run("s3 type without s3 section returns error", func(t *testing.T) {
		r := RepoConfig{
			DataDir:     t.TempDir(),
			TempDir:     t.TempDir(),
			ObjectStore: ObjectStoreConfig{Type: "s3"},
		}
		_, err := r.ToAppConfig()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no object_store.s3 section")
	})

	t.Run("non-s3 type with s3 section configured returns error", func(t *testing.T) {
		r := RepoConfig{
			DataDir: t.TempDir(),
			TempDir: t.TempDir(),
			ObjectStore: ObjectStoreConfig{
				Type: "filesystem",
				S3: &S3Config{
					Endpoint:     "minio.example.com:9000",
					BucketPrefix: "piri-",
				},
			},
		}
		_, err := r.ToAppConfig()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "one backend")
	})

	t.Run("filesystem type with empty data_dir returns error", func(t *testing.T) {
		r := RepoConfig{
			DataDir:     "",
			ObjectStore: ObjectStoreConfig{Type: "filesystem"},
		}
		_, err := r.ToAppConfig()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "data_dir is empty")
	})
}
