package echo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNewEchoSkipsHealthChecks(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	e := NewEcho()
	ok := func(c echo.Context) error { return c.NoContent(http.StatusOK) }
	for _, path := range []string{"/healthz", "/livez", "/readyz", "/other"} {
		e.GET(path, ok)
	}

	for _, path := range []string{"/healthz", "/livez", "/readyz"} {
		e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		require.Empty(t, recorder.Ended(), "%s was traced", path)
	}

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/other", nil))
	require.Len(t, recorder.Ended(), 1, "an ordinary request was not traced")
}
