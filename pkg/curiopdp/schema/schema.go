// Package schema holds Piri's curated PDP closure schema (pdp_* plus the
// harmonytask / eth-message / parked-piece closure — NOT Curio's full
// migration set) and builds harmonydb handles that apply it. It is a leaf
// package so tests anywhere in the tree (including pkg/pdp/service, which
// pkg/curiopdp imports) can spin a schema'd harmonydb without an import
// cycle.
package schema

import (
	"embed"
	"net/url"
	"strings"

	"github.com/filecoin-project/curio/harmony/harmonydb"
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
	})
}
