package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/database"
	"github.com/fil-forge/piri/pkg/database/postgresdb"
	"github.com/fil-forge/piri/pkg/database/sqlitedb"
)

// PostgreSQL schema names for each logical database
const (
	SchemaReplicator    = "replicator"
	SchemaEgressTracker = "egress_tracker"
)

var Module = fx.Module("database",
	fx.Provide(
		fx.Annotate(
			ProvideReplicatorDB,
			fx.ResultTags(`name:"replicator_db"`),
		),
		fx.Annotate(
			ProvideEgressTrackerDB,
			fx.ResultTags(`name:"egress_tracker_db"`),
		),
	),
)

// ProvideReplicatorDB provides the database for the replicator job queue.
// Supports both SQLite (default) and PostgreSQL backends.
func ProvideReplicatorDB(lc fx.Lifecycle, cfg app.StorageConfig) (*sql.DB, error) {
	var db *sql.DB
	var err error

	if cfg.Database.IsPostgres() {
		// Use PostgreSQL with separate schema
		opts := postgresdb.OptionsFromConfig(cfg.Database.Postgres)
		db, err = postgresdb.New(cfg.Database.Postgres.URL.String(), SchemaReplicator, opts...)
		if err != nil {
			return nil, fmt.Errorf("creating postgres replicator database: %w", err)
		}
	} else {
		// Use SQLite (default) - derive path from DataDir
		dbPath := sqliteDBPath(cfg.DataDir, "replicator", "replicator.db")
		if dbPath == "" {
			db, err = sqlitedb.NewMemory()
			if err != nil {
				return nil, fmt.Errorf("creating in-memory replicator database: %w", err)
			}
		} else {
			// Ensure directory exists for file-based database
			if err := ensureSQLiteDir(dbPath); err != nil {
				return nil, fmt.Errorf("creating replicator database directory: %w", err)
			}

			db, err = sqlitedb.New(dbPath,
				database.WithJournalMode(database.JournalModeWAL),
				database.WithTimeout(5*time.Second),
				database.WithSyncMode(database.SyncModeNORMAL),
			)
			if err != nil {
				return nil, fmt.Errorf("creating replicator database: %w", err)
			}
			configureSQLiteConnection(db)
		}
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return db.PingContext(ctx)
		},
		OnStop: func(ctx context.Context) error {
			return db.Close()
		},
	})

	return db, nil
}

// ProvideEgressTrackerDB provides the database for the egress tracker job queue.
// Supports both SQLite (default) and PostgreSQL backends.
func ProvideEgressTrackerDB(lc fx.Lifecycle, cfg app.StorageConfig) (*sql.DB, error) {
	var db *sql.DB
	var err error

	if cfg.Database.IsPostgres() {
		// Use PostgreSQL with separate schema
		opts := postgresdb.OptionsFromConfig(cfg.Database.Postgres)
		db, err = postgresdb.New(cfg.Database.Postgres.URL.String(), SchemaEgressTracker, opts...)
		if err != nil {
			return nil, fmt.Errorf("creating postgres egress tracker database: %w", err)
		}
	} else {
		// Use SQLite (default) - derive path from DataDir
		dbPath := sqliteDBPath(cfg.DataDir, "egress_tracker", "jobqueue", "jobqueue.db")
		if dbPath == "" {
			db, err = sqlitedb.NewMemory()
			if err != nil {
				return nil, fmt.Errorf("creating in-memory egress tracker database: %w", err)
			}
		} else {
			// Ensure directory exists for file-based database
			if err := ensureSQLiteDir(dbPath); err != nil {
				return nil, fmt.Errorf("creating egress tracker database directory: %w", err)
			}

			db, err = sqlitedb.New(dbPath,
				database.WithJournalMode(database.JournalModeWAL),
				database.WithTimeout(5*time.Second),
				database.WithSyncMode(database.SyncModeNORMAL),
			)
			if err != nil {
				return nil, fmt.Errorf("creating egress tracker database: %w", err)
			}
			configureSQLiteConnection(db)
		}
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return db.PingContext(ctx)
		},
		OnStop: func(ctx context.Context) error {
			return db.Close()
		},
	})

	return db, nil
}

// configureSQLiteConnection configures a SQLite database connection with appropriate limits.
// SQLite only supports a single writer, so we limit connections to prevent locking issues.
func configureSQLiteConnection(db *sql.DB) {
	// there can only be ONE connection or sqlite throws a massive tantrum about the
	// database being locked...sobs...wipes tears with mouse pad...
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // Don't expire the connection
}

// sqliteDBPath returns the SQLite database path for the given service.
// Returns empty string for in-memory mode (when dataDir is empty).
func sqliteDBPath(dataDir string, pathElements ...string) string {
	if dataDir == "" {
		return ""
	}
	elements := append([]string{dataDir}, pathElements...)
	return filepath.Join(elements...)
}

// ensureSQLiteDir creates the parent directory for a SQLite database path.
// Returns nil if dbPath is empty (in-memory mode).
func ensureSQLiteDir(dbPath string) error {
	if dbPath == "" {
		return nil
	}
	return os.MkdirAll(filepath.Dir(dbPath), 0755)
}
