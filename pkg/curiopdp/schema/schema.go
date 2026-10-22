// Package schema holds Piri's curated PDP closure schema (pdp_* plus the
// harmonytask / eth-message / parked-piece closure — NOT Curio's full
// migration set) and builds harmonydb handles that apply it. It is a leaf
// package so tests anywhere in the tree (including pkg/pdp/service, which
// pkg/curiopdp imports) can spin a schema'd harmonydb without an import
// cycle.
package schema

import (
	"embed"
	"fmt"
	"net/url"
	"strings"

	"github.com/curiostorage/harmonyquery"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FS holds the curated schema; harmonyquery reads migrations from its "sql/"
// directory.
//
//go:embed sql
var FS embed.FS

// HarmonyDBSchema is the Postgres schema the PDP pipeline tables live in
// (kept separate from Piri's own "scheduler"/gorm schema).
const HarmonyDBSchema = "curio"

// NewDB builds a harmonydb.DB at the given Postgres URL with the curated
// schema applied. Requires Postgres — harmonytask's claim uses FOR UPDATE
// SKIP LOCKED.
//
// The pool is sized from pgx's pool parameters on the URL's query string
// (pool_max_conns, pool_max_conn_lifetime, pool_max_conn_idle_time); any the
// URL omits take pgx's defaults. pool_min_conns is refused: see poolConfig.
func NewDB(u url.URL) (*harmonydb.DB, error) {
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	pass, _ := u.User.Password()
	sslmode := u.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}
	pool, err := poolConfig(u)
	if err != nil {
		return nil, err
	}
	embedFS := FS
	return harmonydb.NewFromConfig(harmonydb.Config{
		Hosts:      []string{u.Hostname()},
		Port:       port,
		Username:   u.User.Username(),
		Password:   pass,
		Database:   strings.TrimPrefix(u.Path, "/"),
		SSLMode:    sslmode,
		Schema:     HarmonyDBSchema,
		SqlEmbedFS: &embedFS,
		PoolConfig: pool,
	})
}

// poolConfig reads pgx's pool parameters from the URL's query string.
// harmonyquery rebuilds its own connection string from host, port, user and
// sslmode, so the pool parameters only reach pgx through PoolConfig, which
// harmonyquery applies all-or-nothing: pgxpool.ParseConfig fills in pgx's
// defaults for any the URL omits, so every field is always set.
//
// A minimum above zero is refused. pgx opens the minimum connections in the
// background before its constructor returns, and harmonyquery's connect hook
// reads a pool field that is assigned only after that return, so an early
// connection panics on a nil pool. With a zero minimum connections open on
// first use, after the field is set.
func poolConfig(u url.URL) (*harmonyquery.PoolConfig, error) {
	cfg, err := pgxpool.ParseConfig(u.String())
	if err != nil {
		return nil, fmt.Errorf("parsing postgres URL pool parameters: %w", err)
	}
	if cfg.MinConns != 0 {
		return nil, fmt.Errorf("postgres URL: pool_min_conns must be omitted or 0, got %d", cfg.MinConns)
	}
	return &harmonyquery.PoolConfig{
		MaxConnections:        int(cfg.MaxConns),
		MinConnections:        int(cfg.MinConns),
		MaxConnectionLifetime: cfg.MaxConnLifetime,
		MaxIdleTime:           cfg.MaxConnIdleTime,
	}, nil
}
