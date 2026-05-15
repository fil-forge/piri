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

// ObjectStoreType represents the object-store backend type.
type ObjectStoreType string

const (
	// ObjectStoreTypeMemory keeps every store in process memory. For tests.
	ObjectStoreTypeMemory ObjectStoreType = "memory"
	// ObjectStoreTypeFilesystem uses local filesystem for every store.
	ObjectStoreTypeFilesystem ObjectStoreType = "filesystem"
	// ObjectStoreTypeS3 uses S3-compatible storage for the bulk stores
	// (blobs, claims, allocations, acceptances, receipts, consolidation).
	// Four stores stay on the local filesystem regardless: KeyStore,
	// AggregatorDatastore, PublisherStore, RetrievalJournal.
	ObjectStoreTypeS3 ObjectStoreType = "s3"
)

// ObjectStoreConfig configures the object-store layer.
//
// A deployment runs with one backend (Type) for the entire object-store
// layer. Four stores (KeyStore, AggregatorDatastore, PublisherStore,
// RetrievalJournal) always live on the local filesystem regardless of the
// chosen backend — their paths live under Local. Only the bulk sub-config
// matching Type is meaningful for the bulk stores; the others are
// zero-valued.
//
// Adding a future backend (e.g. GCS) means adding another sibling sub-struct,
// a new ObjectStoreType constant, and one branch in the StorageModule
// selector.
type ObjectStoreConfig struct {
	// Type is the object-store backend type: "memory", "filesystem", or "s3".
	Type ObjectStoreType

	// Local holds paths for the four stores that always use the local
	// filesystem. Populated for filesystem and s3 backends; zero-valued for
	// memory.
	Local LocalStorePaths

	// Filesystem holds paths for the bulk stores when Type == filesystem.
	Filesystem FilesystemBulkPaths

	// S3 holds S3-compatible storage settings when Type == s3.
	S3 S3Config
}

// IsMemory returns true when the in-memory backend is selected.
func (c ObjectStoreConfig) IsMemory() bool {
	return c.Type == ObjectStoreTypeMemory
}

// IsFilesystem returns true when the filesystem backend is selected (or when
// Type is empty — filesystem is the default for non-test deployments).
func (c ObjectStoreConfig) IsFilesystem() bool {
	return c.Type == "" || c.Type == ObjectStoreTypeFilesystem
}

// IsS3 returns true when the S3 backend is selected.
func (c ObjectStoreConfig) IsS3() bool {
	return c.Type == ObjectStoreTypeS3
}

// LocalStorePaths holds the on-disk directories for stores that always live
// on the local filesystem, regardless of the object-store backend.
type LocalStorePaths struct {
	Aggregator       AggregatorStorageConfig
	Publisher        PublisherStorageConfig
	RetrievalJournal RetrievalJournalConfig
	KeyStore         KeyStoreConfig
}

// FilesystemBulkPaths holds the on-disk directories for the bulk stores when
// running with the filesystem backend.
type FilesystemBulkPaths struct {
	Allocations   AllocationStorageConfig
	Acceptance    AcceptanceStorageConfig
	Claims        ClaimStorageConfig
	Receipts      ReceiptStorageConfig
	PDP           PDPStoreConfig
	Consolidation ConsolidationStorageConfig
}

// StorageConfig contains all storage paths and directories
type StorageConfig struct {
	// Root directories
	DataDir string
	TempDir string

	// Database configuration (sqlite or postgres)
	Database DatabaseConfig

	// ObjectStore configuration (memory, filesystem, or s3)
	ObjectStore ObjectStoreConfig
}

// S3Config configures S3-compatible storage (e.g., MinIO, AWS S3).
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

// RetrievalJournalConfig contains the on-disk directory for the retrieval
// journal — an append-only file-based journal that the egress-tracker
// service consumes.
type RetrievalJournalConfig struct {
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
