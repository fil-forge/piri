package app

import (
	"net/url"
	"time"
)

// DatabaseType represents the database backend type.
type DatabaseType string

const (
	// DatabaseTypeSQLite uses SQLite as the database backend (default).
	DatabaseTypeSQLite DatabaseType = "sqlite"
	// DatabaseTypePostgres uses PostgreSQL as the database backend.
	DatabaseTypePostgres DatabaseType = "postgres"
)

// DatabaseConfig contains database connection configuration.
type DatabaseConfig struct {
	// Type is the database backend type: "sqlite" (default) or "postgres".
	Type DatabaseType

	// NB: sqlite doesn't have a config.

	Postgres PostgresConfig
}

// IsSQLite returns true if using SQLite backend (or if type is empty/default).
func (c DatabaseConfig) IsSQLite() bool {
	return c.Type == "" || c.Type == DatabaseTypeSQLite
}

// IsPostgres returns true if using PostgreSQL backend.
func (c DatabaseConfig) IsPostgres() bool {
	return c.Type == DatabaseTypePostgres
}

type PostgresConfig struct {
	// URL is the PostgreSQL connection string (only used when Type is "postgres").
	// Format: postgres://user:password@host:port/dbname?sslmode=disable
	URL url.URL
	// MaxOpenConns is the maximum number of open connections to the database.
	// Only used for PostgreSQL. Zero means use default (5).
	MaxOpenConns int
	// MaxIdleConns is the maximum number of idle connections in the pool.
	// Only used for PostgreSQL. Zero means use default (5).
	MaxIdleConns int
	// ConnMaxLifetime is the maximum amount of time a connection may be reused.
	// Only used for PostgreSQL. Zero means use default (30 minutes).
	ConnMaxLifetime time.Duration
}

// StorageConfig contains all storage paths and directories
type StorageConfig struct {
	// Root directories
	DataDir string
	TempDir string

	// Database configuration (sqlite or postgres)
	Database DatabaseConfig

	// Global S3 config - when set, blobs are stored in S3, in a bucket named
	// using BucketPrefix (e.g., "piri-pdp")
	S3 *S3Config

	// Service-specific storage subdirectories
	Blobs            BlobStorageConfig
	Publisher        PublisherStorageConfig
	EgressTracker    EgressTrackerStorageConfig
	Replicator       ReplicatorStorageConfig
	KeyStore         KeyStoreConfig
	StashStore       StashStoreConfig
	SchedulerStorage SchedulerConfig
	PDPStore         PDPStoreConfig
}

// S3Config configures S3-compatible storage (e.g., MinIO, AWS S3).
// When set on StorageConfig, blobs are stored in S3.
type S3Config struct {
	Endpoint     string      // API URL (e.g., "minio.example.com:9000")
	BucketPrefix string      // Prefix for bucket names (e.g., "piri-" creates piri-pdp)
	Credentials  Credentials // access credentials
	Insecure     bool        // set to true to disable SSL (for development only)
}

// BlobStorageConfig contains blob-specific storage paths
type BlobStorageConfig struct {
	Dir    string
	TmpDir string
}

// PublisherStorageConfig contains publisher-specific storage paths
type PublisherStorageConfig struct {
	Dir string
}

// EgressTrackerStorageConfig contains egress tracker store-specific storage paths
type EgressTrackerStorageConfig struct {
	Dir string
}

// ReplicatorStorageConfig contains replicator-specific storage paths.
// Currently empty - SQLite paths are derived by providers.
type ReplicatorStorageConfig struct{}

type KeyStoreConfig struct {
	Dir string
}

type StashStoreConfig struct {
	Dir string
}

type PDPStoreConfig struct {
	Dir string
}

// Credentials configures access credentials for S3-compatible storage.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
}

// SchedulerConfig contains scheduler-specific storage paths.
// Currently empty - SQLite paths are derived by providers.
type SchedulerConfig struct{}
