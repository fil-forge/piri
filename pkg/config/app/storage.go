package app

import (
	"net/url"
	"time"
)

// DatabaseConfig contains database connection configuration.
// PostgreSQL is the only supported backend.
type DatabaseConfig struct {
	Postgres PostgresConfig
}

type PostgresConfig struct {
	// URL is the PostgreSQL connection string.
	// Format: postgres://user:password@host:port/dbname?sslmode=disable
	URL url.URL
	// MaxOpenConns is the maximum number of open connections to the database.
	// Zero means use default (5).
	MaxOpenConns int
	// MaxIdleConns is the maximum number of idle connections in the pool.
	// Zero means use default (5).
	MaxIdleConns int
	// ConnMaxLifetime is the maximum amount of time a connection may be reused.
	// Zero means use default (30 minutes).
	ConnMaxLifetime time.Duration
}

// StorageConfig contains all storage paths and directories
type StorageConfig struct {
	// Root directories
	DataDir string
	TempDir string

	// Database configuration (PostgreSQL)
	Database DatabaseConfig

	// Global S3 config - when set, blobs and IPNI adverts are stored in S3,
	// where BucketPrefix or Path places them (see S3Config)
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
// When set on StorageConfig, blobs are stored in S3, where exactly one of
// BucketPrefix and Path places them.
type S3Config struct {
	Endpoint string // API URL (e.g., "minio.example.com:9000")
	// BucketPrefix names a bucket of Piri's own ("piri-" creates piri-pdp).
	BucketPrefix string
	// Path names a bucket shared with other data, optionally followed by a
	// folder ("fil-one/piri-0/" stores under piri-0/pdp/ and piri-0/ipni/).
	Path        string
	Credentials Credentials // access credentials
	Insecure    bool        // set to true to disable SSL (for development only)
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
// Currently empty - the replicator persists via the shared database.
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
// Currently empty - the scheduler persists via the shared database.
type SchedulerConfig struct{}
