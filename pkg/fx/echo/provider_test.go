package echo

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"

	"github.com/fil-forge/piri/pkg/pdp/types"
)

func TestNewEchoSkipsHealthChecks(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	e := NewEcho()
	ok := func(c *echo.Context) error { return c.NoContent(http.StatusOK) }
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

func TestNewEchoRecordsStatusCodeOfErrors(t *testing.T) {
	testCases := []struct {
		name           string
		handlerErr     error
		expectedStatus int
	}{
		{"types.Error of KindNotFound", types.NewError(types.KindNotFound, "no such piece"), http.StatusNotFound},
		{"echo.HTTPError", echo.NewHTTPError(http.StatusBadRequest, "bad input"), http.StatusBadRequest},
		{"echo.ErrNotFound", echo.ErrNotFound, http.StatusNotFound},
		{"plain error", errors.New("boom"), http.StatusInternalServerError},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
			t.Cleanup(func() { otel.SetTracerProvider(previous) })

			e := NewEcho()
			e.GET("/fail", func(c *echo.Context) error { return tc.handlerErr })
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fail", nil))

			require.Equal(t, []int{tc.expectedStatus, tc.expectedStatus}, []int{rec.Code, spanStatusCode(t, recorder)})
		})
	}
}

func spanStatusCode(t *testing.T, recorder *tracetest.SpanRecorder) int {
	t.Helper()
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	for _, kv := range spans[0].Attributes() {
		if kv.Key == semconv.HTTPResponseStatusCodeKey && kv.Value.Type() == attribute.INT64 {
			return int(kv.Value.AsInt64())
		}
	}
	t.Fatal("span has no http.response.status_code attribute")
	return 0
}
