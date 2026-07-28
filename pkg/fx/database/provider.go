package database

import (
	"context"
	"database/sql"
	"fmt"

	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/database/postgresdb"
)

// PostgreSQL schema names for each logical database
const (
	SchemaReplicator    = "replicator"
	SchemaAggregator    = "aggregator"
	SchemaEgressTracker = "egress_tracker"
)

var Module = fx.Module("database",
	fx.Provide(
		fx.Annotate(
			ProvideReplicatorDB,
			fx.ResultTags(`name:"replicator_db"`),
		),
		fx.Annotate(
			ProvideAggregatorDB,
			fx.ResultTags(`name:"aggregator_db"`),
		),
		fx.Annotate(
			ProvideEgressTrackerDB,
			fx.ResultTags(`name:"egress_tracker_db"`),
		),
	),
)

// ProvideReplicatorDB provides the database for the replicator job queue.
func ProvideReplicatorDB(lc fx.Lifecycle, cfg app.StorageConfig) (*sql.DB, error) {
	return providePostgresDB(lc, cfg, SchemaReplicator, "replicator")
}

// ProvideAggregatorDB provides the database for the aggregator job queue.
func ProvideAggregatorDB(lc fx.Lifecycle, cfg app.StorageConfig) (*sql.DB, error) {
	return providePostgresDB(lc, cfg, SchemaAggregator, "aggregator")
}

// ProvideEgressTrackerDB provides the database for the egress tracker job queue.
func ProvideEgressTrackerDB(lc fx.Lifecycle, cfg app.StorageConfig) (*sql.DB, error) {
	return providePostgresDB(lc, cfg, SchemaEgressTracker, "egress tracker")
}

// providePostgresDB opens a PostgreSQL connection scoped to the given schema
// and ties its lifetime to the fx lifecycle.
func providePostgresDB(lc fx.Lifecycle, cfg app.StorageConfig, schema, name string) (*sql.DB, error) {
	opts := postgresdb.OptionsFromConfig(cfg.Database.Postgres)
	db, err := postgresdb.New(cfg.Database.Postgres.URL.String(), schema, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating postgres %s database: %w", name, err)
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
