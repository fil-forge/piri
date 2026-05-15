package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/fil-forge/piri/pkg/config/app"
)

// Credentials configures access credentials for S3-compatible storage.
type Credentials struct {
	AccessKeyID     string `mapstructure:"access_key_id" toml:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key" toml:"secret_access_key"`
}

// S3Config configures S3-compatible storage (e.g., MinIO, AWS S3).
// When configured, all supported stores use S3 with separate buckets
// named using the BucketPrefix (e.g., "piri-blobs", "piri-allocations").
type S3Config struct {
	Endpoint     string      `mapstructure:"endpoint" validate:"required" toml:"endpoint"`
	BucketPrefix string      `mapstructure:"bucket_prefix" validate:"required" toml:"bucket_prefix"`
	Credentials  Credentials `mapstructure:"credentials" toml:"credentials,omitempty"`
	Insecure     bool        `mapstructure:"insecure" toml:"insecure,omitempty"`
}

// IsConfigured returns true if any S3 configuration is provided.
func (c *S3Config) IsConfigured() bool {
	if c == nil {
		return false
	}
	return c.Endpoint != "" || c.BucketPrefix != "" ||
		c.Credentials.AccessKeyID != "" || c.Credentials.SecretAccessKey != "" ||
		c.Insecure
}

// Validate checks that S3 configuration is complete.
// Returns an error if S3 is partially configured (e.g., endpoint without bucket_prefix).
func (c *S3Config) Validate() error {
	if c == nil {
		return nil
	}
	// Check if any S3 config is provided
	if !c.IsConfigured() {
		return nil
	}
	// If any S3 config is provided, endpoint and bucket_prefix are required
	if c.Endpoint == "" {
		return errors.New("s3 endpoint is required when S3 storage is configured")
	}
	if c.BucketPrefix == "" {
		return errors.New("s3 bucket_prefix is required when S3 storage is configured")
	}
	return nil
}

// DatabaseConfig configures the database backend.
type DatabaseConfig struct {
	// Type is the database backend: "sqlite" (default) or "postgres"
	Type     string         `mapstructure:"type" validate:"omitempty,oneof=sqlite postgres" toml:"type,omitempty"`
	Postgres PostgresConfig `mapstructure:"postgres" validate:"omitempty" toml:"postgres,omitempty"`
}

// ToAppConfig converts DatabaseConfig to app.DatabaseConfig. SQLite paths are
// populated by the caller (RepoConfig.ToAppConfig) since they depend on the
// repo's data directory.
//
// A deployment runs with exactly one database backend; configuring the
// postgres section while leaving type as sqlite (or empty) is a configuration
// error and surfaces here rather than getting silently discarded.
func (c DatabaseConfig) ToAppConfig() (app.DatabaseConfig, error) {
	if c.Type == "postgres" {
		pgCfg, err := c.Postgres.ToAppConfig()
		if err != nil {
			return app.DatabaseConfig{}, err
		}
		return app.DatabaseConfig{
			Type:     app.DatabaseTypePostgres,
			Postgres: pgCfg,
		}, nil
	}
	if c.Postgres.URL != "" {
		return app.DatabaseConfig{}, fmt.Errorf(
			"database.postgres section is configured but database.type is %q; "+
				"a deployment uses one backend for all databases — set type to \"postgres\" or remove the postgres section",
			c.Type,
		)
	}
	return app.DatabaseConfig{
		Type: app.DatabaseTypeSQLite,
	}, nil
}

type PostgresConfig struct {
	// URL is the PostgreSQL connection string (only used when type is "postgres")
	// Format: postgres://user:password@host:port/dbname?sslmode=disable
	URL string `mapstructure:"url" flag:"db-url" toml:"url,omitempty"`
	// MaxOpenConns is the maximum number of open connections to the database.
	// Only used for PostgreSQL. Default: 5
	MaxOpenConns int `mapstructure:"max_open_conns" toml:"max_open_conns,omitempty"`
	// MaxIdleConns is the maximum number of idle connections in the pool.
	// Only used for PostgreSQL. Default: 5
	MaxIdleConns int `mapstructure:"max_idle_conns" toml:"max_idle_conns,omitempty"`
	// ConnMaxLifetime is the maximum amount of time a connection may be reused.
	// Only used for PostgreSQL. Accepts Go duration strings (e.g., "30m", "1h"). Default: "30m"
	ConnMaxLifetime string `mapstructure:"conn_max_lifetime" toml:"conn_max_lifetime,omitempty"`
}

// ToAppConfig converts PostgresConfig to app.PostgresConfig.
// Parses the URL string and duration string into their typed equivalents.
func (c PostgresConfig) ToAppConfig() (app.PostgresConfig, error) {
	if c.URL == "" {
		return app.PostgresConfig{}, errors.New("postgres URL is required")
	}
	pgurl, err := url.Parse(c.URL)
	if err != nil {
		return app.PostgresConfig{}, fmt.Errorf("invalid postgres URL %q: %w", c.URL, err)
	}

	var connMaxLifetime time.Duration
	if c.ConnMaxLifetime != "" {
		connMaxLifetime, err = time.ParseDuration(c.ConnMaxLifetime)
		if err != nil {
			return app.PostgresConfig{}, fmt.Errorf("invalid conn_max_lifetime %q: %w", c.ConnMaxLifetime, err)
		}
	}

	return app.PostgresConfig{
		URL:             *pgurl,
		MaxOpenConns:    c.MaxOpenConns,
		MaxIdleConns:    c.MaxIdleConns,
		ConnMaxLifetime: connMaxLifetime,
	}, nil
}

type RepoConfig struct {
	DataDir     string            `mapstructure:"data_dir" validate:"required" flag:"data-dir" toml:"data_dir"`
	TempDir     string            `mapstructure:"temp_dir" validate:"required" flag:"temp-dir" toml:"temp_dir"`
	Database    DatabaseConfig    `mapstructure:"database" validate:"omitempty" toml:"database,omitempty"`
	ObjectStore ObjectStoreConfig `mapstructure:"object_store" validate:"omitempty" toml:"object_store,omitempty"`
}

// ObjectStoreConfig configures the object-store layer at the TOML/YAML
// level. A deployment runs with exactly one backend selected by Type;
// mixed-backend deployments are not supported.
type ObjectStoreConfig struct {
	// Type selects the backend: "memory", "filesystem", or "s3".
	Type string `mapstructure:"type" validate:"omitempty,oneof=memory filesystem s3" toml:"type,omitempty"`
	// S3 holds S3-compatible storage settings. Only meaningful when Type is "s3".
	S3 *S3Config `mapstructure:"s3" validate:"omitempty" toml:"s3,omitempty"`
}

func (r RepoConfig) Validate() error {
	return validateConfig(r)
}

func (r RepoConfig) ToAppConfig() (app.StorageConfig, error) {
	dbCfg, err := r.Database.ToAppConfig()
	if err != nil {
		return app.StorageConfig{}, fmt.Errorf("database config: %w", err)
	}

	objCfg, err := r.toObjectStoreAppConfig()
	if err != nil {
		return app.StorageConfig{}, fmt.Errorf("object_store config: %w", err)
	}

	if r.DataDir == "" {
		// Return empty config for memory stores
		return app.StorageConfig{
			Database:    dbCfg,
			ObjectStore: objCfg,
		}, nil
	}

	// Ensure directories exist
	if err := os.MkdirAll(r.DataDir, 0755); err != nil {
		return app.StorageConfig{}, err
	}
	if err := os.MkdirAll(r.TempDir, 0755); err != nil {
		return app.StorageConfig{}, err
	}

	// SQLite databases each live in their own file under the data dir. Paths
	// are computed here rather than derived inside fx providers so that the
	// config fully describes the on-disk layout. Populated only when the
	// selected backend is SQLite — deployments pick one backend for the
	// entire database layer; the inactive sub-config stays zero-valued.
	if dbCfg.IsSQLite() {
		dbCfg.SQLite = app.SQLiteConfig{
			ReplicatorPath:    filepath.Join(r.DataDir, "replicator", "replicator.db"),
			AggregatorPath:    filepath.Join(r.DataDir, "aggregator", "jobqueue", "jobqueue.db"),
			EgressTrackerPath: filepath.Join(r.DataDir, "egress_tracker", "jobqueue", "jobqueue.db"),
			TaskEnginePath:    filepath.Join(r.DataDir, "pdp", "state", "state.db"),
		}
	}

	// Local-only store paths are populated for filesystem and s3 backends;
	// they describe four stores that always live on disk regardless of the
	// bulk-store backend.
	if !objCfg.IsMemory() {
		objCfg.Local = app.LocalStorePaths{
			Aggregator:       app.AggregatorStorageConfig{Dir: filepath.Join(r.DataDir, "aggregator", "datastore")},
			Publisher:        app.PublisherStorageConfig{Dir: filepath.Join(r.DataDir, "publisher")},
			RetrievalJournal: app.RetrievalJournalConfig{Dir: filepath.Join(r.DataDir, "retrieval_journal")},
			KeyStore:         app.KeyStoreConfig{Dir: filepath.Join(r.DataDir, "wallet")},
		}
	}

	// Filesystem bulk paths populated only for the filesystem backend.
	if objCfg.IsFilesystem() {
		objCfg.Filesystem = app.FilesystemBulkPaths{
			Allocations:   app.AllocationStorageConfig{Dir: filepath.Join(r.DataDir, "allocation")},
			Acceptance:    app.AcceptanceStorageConfig{Dir: filepath.Join(r.DataDir, "acceptance")},
			Claims:        app.ClaimStorageConfig{Dir: filepath.Join(r.DataDir, "claim")},
			Receipts:      app.ReceiptStorageConfig{Dir: filepath.Join(r.DataDir, "receipt")},
			PDP:           app.PDPStoreConfig{Dir: filepath.Join(r.DataDir, "pdp", "datastore")},
			Consolidation: app.ConsolidationStorageConfig{Dir: filepath.Join(r.DataDir, "consolidation")},
		}
	}

	return app.StorageConfig{
		DataDir:     r.DataDir,
		TempDir:     r.TempDir,
		Database:    dbCfg,
		ObjectStore: objCfg,
	}, nil
}

// toObjectStoreAppConfig resolves the repo-level ObjectStoreConfig to an
// app.ObjectStoreConfig with a normalized Type. It validates the cross-field
// invariants — Type and Backend-section presence must agree — and copies the
// S3 sub-section through when Type == s3. Filesystem and Local paths are
// populated by the caller from DataDir.
func (r RepoConfig) toObjectStoreAppConfig() (app.ObjectStoreConfig, error) {
	if err := r.ObjectStore.S3.Validate(); err != nil {
		return app.ObjectStoreConfig{}, err
	}

	// Default selection: memory when DataDir is empty (test mode), filesystem
	// otherwise. The user can override with an explicit type.
	typ := app.ObjectStoreType(r.ObjectStore.Type)
	if typ == "" {
		if r.DataDir == "" {
			typ = app.ObjectStoreTypeMemory
		} else {
			typ = app.ObjectStoreTypeFilesystem
		}
	}

	// Cross-field validation: the s3 section is only meaningful when Type
	// is s3. Configuring it under any other type is a user mistake — a
	// deployment runs with one backend for the entire object-store layer.
	if typ != app.ObjectStoreTypeS3 && r.ObjectStore.S3.IsConfigured() {
		return app.ObjectStoreConfig{}, fmt.Errorf(
			"object_store.s3 section is configured but object_store.type is %q; "+
				"a deployment uses one backend for the object-store layer — set type to \"s3\" or remove the s3 section",
			typ,
		)
	}
	if typ == app.ObjectStoreTypeS3 && !r.ObjectStore.S3.IsConfigured() {
		return app.ObjectStoreConfig{}, fmt.Errorf(
			"object_store.type is \"s3\" but no object_store.s3 section is configured",
		)
	}
	if typ == app.ObjectStoreTypeFilesystem && r.DataDir == "" {
		return app.ObjectStoreConfig{}, fmt.Errorf(
			"object_store.type is \"filesystem\" but data_dir is empty",
		)
	}

	out := app.ObjectStoreConfig{Type: typ}
	if typ == app.ObjectStoreTypeS3 {
		out.S3 = app.S3Config{
			Endpoint:     r.ObjectStore.S3.Endpoint,
			BucketPrefix: r.ObjectStore.S3.BucketPrefix,
			Credentials: app.Credentials{
				AccessKeyID:     r.ObjectStore.S3.Credentials.AccessKeyID,
				SecretAccessKey: r.ObjectStore.S3.Credentials.SecretAccessKey,
			},
			Insecure: r.ObjectStore.S3.Insecure,
		}
	}
	return out, nil
}
