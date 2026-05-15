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
//
// A deployment runs with exactly one database backend: every logical database
// (replicator queue, aggregator queue, egress-tracker queue, task engine
// state) uses the backend selected by Type. Mixed-backend deployments — e.g.
// running the task engine on Postgres while job queues stay on SQLite — are
// not supported. Only the sub-config for the selected backend is meaningful;
// the others are zero-valued.
//
// Adding a future backend (e.g. Yugabyte) means adding another sibling
// sub-struct, a new DatabaseType constant, and one branch in the providers
// that consume this config.
type DatabaseConfig struct {
	// Type is the database backend type: "sqlite" (default) or "postgres".
	// It selects the backend for every logical database in the deployment.
	Type DatabaseType

	// SQLite configuration. Populated only when Type == DatabaseTypeSQLite.
	SQLite SQLiteConfig

	// Postgres configuration. Populated only when Type == DatabaseTypePostgres.
	Postgres PostgresConfig
}

// SQLiteConfig contains SQLite-specific configuration. SQLite uses a separate
// database file per logical namespace (one job queue per file, one task
// engine state file). Paths are populated by the config loader from the
// configured data directory; an empty path means use in-memory storage.
type SQLiteConfig struct {
	ReplicatorPath    string
	AggregatorPath    string
	EgressTrackerPath string
	TaskEnginePath    string
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

	// Global S3 config - when set, all supported stores use S3 with separate buckets
	// named using BucketPrefix (e.g., "piri-blobs", "piri-allocations")
	S3 *S3Config

	// Service-specific storage subdirectories
	Aggregator    AggregatorStorageConfig
	Blobs         BlobStorageConfig
	Claims        ClaimStorageConfig
	Publisher     PublisherStorageConfig
	Receipts      ReceiptStorageConfig
	EgressTracker EgressTrackerStorageConfig
	Allocations   AllocationStorageConfig
	Acceptance    AcceptanceStorageConfig
	KeyStore      KeyStoreConfig
	StashStore    StashStoreConfig
	PDPStore      PDPStoreConfig
	Consolidation ConsolidationStorageConfig
}

// S3Config configures S3-compatible storage (e.g., MinIO, AWS S3).
// When set on StorageConfig, all supported stores use S3 with separate buckets.
type S3Config struct {
	Endpoint     string      // API URL (e.g., "minio.example.com:9000")
	BucketPrefix string      // Prefix for bucket names (e.g., "piri-" creates piri-blobs, piri-allocations, etc.)
	Credentials  Credentials // access credentials
	Insecure     bool        // set to true to disable SSL (for development only)
}

// AggregatorStorageConfig contains aggregator-specific storage paths
type AggregatorStorageConfig struct {
	Dir string
}

// BlobStorageConfig contains blob-specific storage paths
type BlobStorageConfig struct {
	Dir    string
	TmpDir string
}

// ClaimStorageConfig contains claim-specific storage paths
type ClaimStorageConfig struct {
	Dir string
}

// PublisherStorageConfig contains publisher-specific storage paths
type PublisherStorageConfig struct {
	Dir string
}

// ReceiptStorageConfig contains receipt-specific storage paths
type ReceiptStorageConfig struct {
	Dir string
}

// EgressTrackerStorageConfig contains egress tracker store-specific storage paths
type EgressTrackerStorageConfig struct {
	Dir string
}

// AllocationStorageConfig contains allocation-specific storage paths
type AllocationStorageConfig struct {
	Dir string
}

// AcceptanceStorageConfig contains acceptance-specific storage paths
type AcceptanceStorageConfig struct {
	Dir string
}

type KeyStoreConfig struct {
	Dir string
}

type StashStoreConfig struct {
	Dir string
}

type PDPStoreConfig struct {
	Dir string
}

// ConsolidationStorageConfig contains consolidation-specific storage paths
type ConsolidationStorageConfig struct {
	Dir string
}

// Credentials configures access credentials for S3-compatible storage.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
}
