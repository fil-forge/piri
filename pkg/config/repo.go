package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fil-forge/piri/pkg/config/app"
)

// Credentials configures access credentials for S3-compatible storage.
type Credentials struct {
	AccessKeyID     string `mapstructure:"access_key_id" toml:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key" toml:"secret_access_key"`
}

// S3Config configures S3-compatible storage (e.g., MinIO, AWS S3).
// When configured, blobs and IPNI adverts are stored in S3, where exactly one
// of BucketPrefix and Path places them.
type S3Config struct {
	Endpoint string `mapstructure:"endpoint" validate:"required" toml:"endpoint"`
	// BucketPrefix names a bucket of Piri's own, {prefix}pdp (e.g. "piri-"
	// creates piri-pdp). IPNI adverts go under ipni/ in that bucket.
	BucketPrefix string `mapstructure:"bucket_prefix" toml:"bucket_prefix,omitempty"`
	// Path places the objects in a bucket shared with other data: the bucket
	// name, optionally followed by a folder that ends with "/". With
	// "fil-one/piri-0/", blobs go under piri-0/pdp/ and IPNI adverts under
	// piri-0/ipni/ in the bucket fil-one.
	Path        string      `mapstructure:"path" toml:"path,omitempty"`
	Credentials Credentials `mapstructure:"credentials" toml:"credentials,omitempty"`
	Insecure    bool        `mapstructure:"insecure" toml:"insecure,omitempty"`
}

// IsConfigured returns true if any S3 configuration is provided.
func (c *S3Config) IsConfigured() bool {
	if c == nil {
		return false
	}
	return c.Endpoint != "" || c.BucketPrefix != "" || c.Path != "" ||
		c.Credentials.AccessKeyID != "" || c.Credentials.SecretAccessKey != "" ||
		c.Insecure
}

// Validate checks that S3 configuration is complete.
// Returns an error if S3 is partially configured (e.g., endpoint without
// bucket_prefix or path), or if both bucket_prefix and path are set.
func (c *S3Config) Validate() error {
	if c == nil {
		return nil
	}
	// Check if any S3 config is provided
	if !c.IsConfigured() {
		return nil
	}
	if c.Endpoint == "" {
		return errors.New("s3 endpoint is required when S3 storage is configured")
	}
	switch {
	case c.BucketPrefix != "" && c.Path != "":
		return errors.New("s3 bucket_prefix and path are mutually exclusive; set only one of them")
	case c.BucketPrefix == "" && c.Path == "":
		return errors.New("s3 bucket_prefix or path is required when S3 storage is configured")
	case strings.Contains(c.BucketPrefix, "/"):
		return fmt.Errorf("s3 bucket_prefix %q cannot contain \"/\"; set path to keep the objects in a folder of a bucket", c.BucketPrefix)
	case c.Path != "":
		return validateS3Path(c.Path)
	}
	return nil
}

// validateS3Path checks that path names a bucket and, if it has a folder,
// that the folder ends with "/".
func validateS3Path(path string) error {
	bucket, folder, _ := strings.Cut(path, "/")
	if bucket == "" {
		return fmt.Errorf("s3 path %q must start with a bucket name", path)
	}
	if folder != "" && !strings.HasSuffix(folder, "/") {
		return fmt.Errorf("s3 path %q must end with \"/\" after its folder", path)
	}
	return nil
}

// DatabaseConfig configures the database backend.
// PostgreSQL is the only supported backend: the PDP pipeline (Curio's
// harmonydb/harmonytask) requires it.
type DatabaseConfig struct {
	Postgres PostgresConfig `mapstructure:"postgres" validate:"omitempty" toml:"postgres,omitempty"`
}

// ToAppConfig converts DatabaseConfig to app.DatabaseConfig.
func (c DatabaseConfig) ToAppConfig() (app.DatabaseConfig, error) {
	pgCfg, err := c.Postgres.ToAppConfig()
	if err != nil {
		return app.DatabaseConfig{}, err
	}
	return app.DatabaseConfig{
		Postgres: pgCfg,
	}, nil
}

type PostgresConfig struct {
	// URL is the PostgreSQL connection string.
	// Format: postgres://user:password@host:port/dbname?sslmode=disable
	// pgx pool parameters on the query string (pool_max_conns,
	// pool_max_conn_lifetime, pool_max_conn_idle_time) size the PDP pipeline's pool.
	URL string `mapstructure:"url" flag:"db-url" toml:"url,omitempty"`
	// MaxOpenConns is the maximum number of open connections to the database.
	// Default: 5
	MaxOpenConns int `mapstructure:"max_open_conns" toml:"max_open_conns,omitempty"`
	// MaxIdleConns is the maximum number of idle connections in the pool.
	// Default: 5
	MaxIdleConns int `mapstructure:"max_idle_conns" toml:"max_idle_conns,omitempty"`
	// ConnMaxLifetime is the maximum amount of time a connection may be reused.
	// Accepts Go duration strings (e.g., "30m", "1h"). Default: "30m"
	ConnMaxLifetime string `mapstructure:"conn_max_lifetime" toml:"conn_max_lifetime,omitempty"`
}

// ToAppConfig converts PostgresConfig to app.PostgresConfig.
// Parses the URL string and duration string into their typed equivalents.
func (c PostgresConfig) ToAppConfig() (app.PostgresConfig, error) {
	if c.URL == "" {
		return app.PostgresConfig{}, errors.New("postgres URL is required (PostgreSQL is the only supported database; SQLite is no longer supported)")
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
	DataDir  string         `mapstructure:"data_dir" validate:"required" flag:"data-dir" toml:"data_dir"`
	TempDir  string         `mapstructure:"temp_dir" validate:"required" flag:"temp-dir" toml:"temp_dir"`
	BlobDir  string         `mapstructure:"blob_dir" flag:"blob-dir" toml:"blob_dir,omitempty"`
	Database DatabaseConfig `mapstructure:"database" validate:"omitempty" toml:"database,omitempty"`
	S3       *S3Config      `mapstructure:"s3" validate:"omitempty" toml:"s3,omitempty"`
}

func (r RepoConfig) Validate() error {
	return validateConfig(r)
}

func (r RepoConfig) ToAppConfig() (app.StorageConfig, error) {
	dbCfg, err := r.Database.ToAppConfig()
	if err != nil {
		return app.StorageConfig{}, fmt.Errorf("database config: %w", err)
	}

	// Validate S3 config if provided
	if err := r.S3.Validate(); err != nil {
		return app.StorageConfig{}, fmt.Errorf("s3 config: %w", err)
	}

	if r.DataDir == "" {
		// Return empty config for memory stores
		return app.StorageConfig{
			Database: dbCfg,
		}, nil
	}

	// Ensure directories exist
	if err := os.MkdirAll(r.DataDir, 0755); err != nil {
		return app.StorageConfig{}, err
	}
	if err := os.MkdirAll(r.TempDir, 0755); err != nil {
		return app.StorageConfig{}, err
	}

	// Build storage config - database paths are derived by providers, not set here
	out := app.StorageConfig{
		DataDir:  r.DataDir,
		TempDir:  r.TempDir,
		Database: dbCfg,
		Blobs: app.BlobStorageConfig{
			Dir:    filepath.Join(r.DataDir, "blobs"),
			TmpDir: filepath.Join(r.TempDir, "storage"),
		},
		Publisher: app.PublisherStorageConfig{
			Dir: filepath.Join(r.blobDir(), "ipni"),
		},
		EgressTracker: app.EgressTrackerStorageConfig{
			Dir: filepath.Join(r.DataDir, "egress_tracker", "journal"),
		},
		Replicator: app.ReplicatorStorageConfig{},
		KeyStore: app.KeyStoreConfig{
			Dir: filepath.Join(r.DataDir, "wallet"),
		},
		StashStore: app.StashStoreConfig{
			Dir: filepath.Join(r.DataDir, "pdp"),
		},
		SchedulerStorage: app.SchedulerConfig{},
		PDPStore: app.PDPStoreConfig{
			Dir: filepath.Join(r.blobDir(), "pdp", "datastore"),
		},
	}

	// Copy S3 config if configured (already validated above)
	if r.S3.IsConfigured() {
		out.S3 = &app.S3Config{
			Endpoint:     r.S3.Endpoint,
			BucketPrefix: r.S3.BucketPrefix,
			Path:         r.S3.Path,
			Credentials: app.Credentials{
				AccessKeyID:     r.S3.Credentials.AccessKeyID,
				SecretAccessKey: r.S3.Credentials.SecretAccessKey,
			},
			Insecure: r.S3.Insecure,
		}
	}

	return out, nil
}

// blobDir is where the filesystem backend keeps blob bytes and IPNI adverts,
// the data that the S3 backend keeps in its bucket.
func (r RepoConfig) blobDir() string {
	if r.BlobDir != "" {
		return r.BlobDir
	}
	return r.DataDir
}
