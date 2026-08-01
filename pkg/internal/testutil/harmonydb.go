package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/fil-forge/piri/pkg/curiopdp/schema"
)

// Shared Postgres container for harmonydb-backed tests, started lazily on
// first use (pattern of lib/jobqueue/internal/testing). Each test gets its
// own freshly-created database with the curated PDP closure schema applied,
// so tests are fully isolated.
var (
	harmonyPGOnce sync.Once
	harmonyPGURL  url.URL // admin connection URL (testdb)
	harmonyPGErr  error
	harmonyDBSeq  atomic.Int64
)

func startHarmonyPostgres() {
	harmonyPGOnce.Do(func() {
		if runtime.GOOS == "darwin" {
			harmonyPGErr = fmt.Errorf("skipping postgres-backed tests on darwin")
			return
		}
		ctx := context.Background()
		container, err := postgres.Run(ctx,
			"postgres:16-alpine",
			postgres.WithDatabase("testdb"),
			postgres.WithUsername("test"),
			postgres.WithPassword("test"),
			// Postgres containers log readiness twice; only the second one
			// means the server is actually accepting connections.
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2)),
		)
		if err != nil {
			harmonyPGErr = fmt.Errorf("starting postgres container: %w", err)
			return
		}
		connStr, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			harmonyPGErr = fmt.Errorf("getting connection string: %w", err)
			return
		}
		u, err := url.Parse(connStr)
		if err != nil {
			harmonyPGErr = fmt.Errorf("parsing connection string: %w", err)
			return
		}
		harmonyPGURL = *u
	})
}

// NewHarmonyDB returns a harmonydb.DB on a fresh database with piri's
// curated PDP schema applied. Skips the test when Docker/Postgres is
// unavailable.
func NewHarmonyDB(t *testing.T) *harmonydb.DB {
	t.Helper()
	startHarmonyPostgres()
	if harmonyPGErr != nil {
		t.Skipf("postgres unavailable: %v", harmonyPGErr)
	}

	admin, err := sql.Open("pgx", harmonyPGURL.String())
	if err != nil {
		t.Fatalf("connecting to admin database: %v", err)
	}
	defer admin.Close()

	dbName := fmt.Sprintf("piri_test_%d", harmonyDBSeq.Add(1))
	if _, err := admin.Exec("CREATE DATABASE " + dbName); err != nil {
		t.Fatalf("creating test database: %v", err)
	}

	u := harmonyPGURL
	u.Path = "/" + dbName
	db, err := schema.NewDB(u)
	if err != nil {
		t.Fatalf("building harmonydb: %v", err)
	}
	// harmonyquery.DB exposes no Close; each test's pool dies with the
	// process, and the container is removed by testcontainers' reaper.
	return db
}
