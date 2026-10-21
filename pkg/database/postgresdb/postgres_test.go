package postgresdb

import (
	"context"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Queries are spans under the caller's span.
func TestQueriesAreTraced(t *testing.T) {
	if os.Getenv("CI") != "" && runtime.GOOS == "darwin" {
		t.Skip("testcontainers not supported in CI on darwin")
	}
	ctx := context.Background()
	pg, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2)),
	)
	testcontainers.CleanupContainer(t, pg)
	require.NoError(t, err)
	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	rec := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	db, err := New(connStr, "traced")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	qctx, parent := otel.Tracer("test").Start(ctx, "blob.accept")
	var one int
	require.NoError(t, db.QueryRowContext(qctx, "SELECT 1").Scan(&one))
	parent.End()

	for _, sp := range rec.Ended() {
		if sp.Parent().SpanID() == parent.SpanContext().SpanID() {
			return
		}
	}
	t.Fatalf("expected a query span under the caller's span, got %d spans", len(rec.Ended()))
}
