package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/fx"
	"gorm.io/gorm"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/database"
	"github.com/fil-forge/piri/pkg/database/gormdb"
	"github.com/fil-forge/piri/pkg/database/postgresdb"
	"github.com/fil-forge/piri/pkg/database/sqlitedb"
)

// PostgreSQL schema names for each logical database.
const (
	SchemaReplicator    = "replicator"
	SchemaAggregator    = "aggregator"
	SchemaEgressTracker = "egress_tracker"
	SchemaScheduler     = "scheduler"
)

var Module = fx.Module("database",
	fx.Provide(
		fx.Annotate(ProvideReplicatorDB, fx.ResultTags(`name:"replicator_db"`)),
		fx.Annotate(ProvideTaskEngineDB, fx.ResultTags(`name:"engine_db"`)),
		fx.Annotate(ProvideAggregatorDB, fx.ResultTags(`name:"aggregator_db"`)),
		fx.Annotate(ProvideEgressTrackerDB, fx.ResultTags(`name:"egress_tracker_db"`)),
	),
)

// ProvideReplicatorDB provides the database for the replicator job queue.
func ProvideReplicatorDB(lc fx.Lifecycle, cfg app.DatabaseConfig) (*sql.DB, error) {
	return newJobQueueDB(lc, cfg, SchemaReplicator, cfg.SQLite.ReplicatorPath)
}

// ProvideAggregatorDB provides the database for the aggregator job queue.
func ProvideAggregatorDB(lc fx.Lifecycle, cfg app.DatabaseConfig) (*sql.DB, error) {
	return newJobQueueDB(lc, cfg, SchemaAggregator, cfg.SQLite.AggregatorPath)
}

// ProvideEgressTrackerDB provides the database for the egress tracker job queue.
func ProvideEgressTrackerDB(lc fx.Lifecycle, cfg app.DatabaseConfig) (*sql.DB, error) {
	return newJobQueueDB(lc, cfg, SchemaEgressTracker, cfg.SQLite.EgressTrackerPath)
}

// newJobQueueDB opens a *sql.DB for one of the job-queue databases. schema
// is the Postgres schema name; sqlitePath is the SQLite file path (empty
// path means in-memory).
func newJobQueueDB(lc fx.Lifecycle, cfg app.DatabaseConfig, schema, sqlitePath string) (*sql.DB, error) {
	var db *sql.DB
	var err error

	if cfg.IsPostgres() {
		opts := postgresdb.OptionsFromConfig(cfg.Postgres)
		db, err = postgresdb.New(cfg.Postgres.URL.String(), schema, opts...)
		if err != nil {
			return nil, fmt.Errorf("creating postgres %s database: %w", schema, err)
		}
	} else if sqlitePath == "" {
		db, err = sqlitedb.NewMemory()
		if err != nil {
			return nil, fmt.Errorf("creating in-memory %s database: %w", schema, err)
		}
	} else {
		if err := ensureSQLiteDir(sqlitePath); err != nil {
			return nil, fmt.Errorf("creating %s database directory: %w", schema, err)
		}
		db, err = sqlitedb.New(sqlitePath,
			database.WithJournalMode(database.JournalModeWAL),
			database.WithTimeout(5*time.Second),
			database.WithSyncMode(database.SyncModeNORMAL),
		)
		if err != nil {
			return nil, fmt.Errorf("creating %s database: %w", schema, err)
		}
		configureSQLiteConnection(db)
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error { return db.PingContext(ctx) },
		OnStop:  func(ctx context.Context) error { return db.Close() },
	})
	return db, nil
}

// ProvideTaskEngineDB provides the GORM database for the task engine scheduler.
// It uses a different SQLite option profile than the job-queue databases (foreign
// keys enabled, journal mode depends on file vs in-memory) and returns *gorm.DB
// rather than *sql.DB, so it stays separate from newJobQueueDB.
func ProvideTaskEngineDB(lc fx.Lifecycle, cfg app.DatabaseConfig) (*gorm.DB, error) {
	var db *gorm.DB
	var err error

	if cfg.IsPostgres() {
		opts := gormdb.PostgresOptionsFromConfig(cfg.Postgres)
		db, err = gormdb.NewPostgres(cfg.Postgres.URL.String(), SchemaScheduler, opts)
		if err != nil {
			return nil, fmt.Errorf("creating postgres task engine db: %w", err)
		}
	} else {
		dbPath := cfg.SQLite.TaskEnginePath
		dbOpts := []database.Option{
			// ensure foreign key constraints are respected.
			database.WithForeignKeyConstraintsEnable(true),
			// wait up to 5 seconds before failing to write due to busted database.
			database.WithTimeout(5 * time.Second),
		}
		if dbPath == "" {
			dbPath = "file::memory:?cache=shared"
			// use an in-memory cache for in-memory database
			dbOpts = append(dbOpts, database.WithJournalMode(database.JournalModeMEMORY))
		} else {
			if err := ensureSQLiteDir(dbPath); err != nil {
				return nil, fmt.Errorf("creating task engine database directory: %w", err)
			}
			// use a write ahead log for transactions, good for parallel operations on persisted databases
			dbOpts = append(dbOpts, database.WithJournalMode(database.JournalModeWAL))
		}

		db, err = gormdb.New(dbPath, dbOpts...)
		if err != nil {
			return nil, fmt.Errorf("creating task engine db: %w", err)
		}

		// Ensure single connection for SQLite to prevent locking issues
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("getting underlying sql.DB: %w", err)
		}
		configureSQLiteConnection(sqlDB)
	}

	lc.Append(fx.Hook{
		// NB(forrest): we don't ping the gorm database on startup since the gorm package does so internally.
		OnStop: func(ctx context.Context) error {
			ddb, err := db.DB()
			if err != nil {
				return fmt.Errorf("stopping task engine db: %w", err)
			}
			if err := ddb.Close(); err != nil {
				return fmt.Errorf("stopping task engine db: %w", err)
			}
			return nil
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

// ensureSQLiteDir creates the parent directory for a SQLite database path.
func ensureSQLiteDir(dbPath string) error {
	return os.MkdirAll(filepath.Dir(dbPath), 0755)
}
