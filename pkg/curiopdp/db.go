package curiopdp

import (
	"fmt"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/curiopdp/schema"
)

// ProvideHarmonyDB builds a harmonydb.DB (harmonyquery on Postgres) that applies
// only the curated PDP closure schema (see pkg/curiopdp/schema). Curio's pdpv0
// tasks + harmonytask run on this DB. Requires Postgres — harmonytask's claim
// uses FOR UPDATE SKIP LOCKED.
func ProvideHarmonyDB(cfg app.StorageConfig) (*harmonydb.DB, error) {
	if !cfg.Database.IsPostgres() {
		return nil, fmt.Errorf("curio PDP pipeline requires Postgres (set database type to postgres)")
	}
	return schema.NewDB(cfg.Database.Postgres.URL)
}
