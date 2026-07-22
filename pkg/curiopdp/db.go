package curiopdp

import (
	"embed"
	"fmt"
	"strings"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/fil-forge/piri/pkg/config/app"
)

// curatedSchemaFS holds ONLY the consolidated PDP closure schema (pdp_* + the
// harmonytask / eth-message / parked-piece closure), not Curio's full migration
// set. harmonyquery reads migrations from the "sql/" directory of this FS.
//
//go:embed sql
var curatedSchemaFS embed.FS

// HarmonyDBSchema is the Postgres schema the PDP pipeline tables live in (kept
// separate from Piri's own "scheduler"/gorm schema).
const HarmonyDBSchema = "curio"

// ProvideHarmonyDB builds a harmonydb.DB (harmonyquery on Postgres) that applies
// only the curated PDP closure schema. Curio's pdpv0 tasks + harmonytask run on
// this DB. Requires Postgres — harmonytask's claim uses FOR UPDATE SKIP LOCKED.
func ProvideHarmonyDB(cfg app.StorageConfig) (*harmonydb.DB, error) {
	if !cfg.Database.IsPostgres() {
		return nil, fmt.Errorf("curio PDP pipeline requires Postgres (set database type to postgres)")
	}
	u := cfg.Database.Postgres.URL
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	pass, _ := u.User.Password()
	sslmode := u.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}
	embedFS := curatedSchemaFS
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
