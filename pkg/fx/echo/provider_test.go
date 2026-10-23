package echo

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	logging "github.com/ipfs/go-log/v2"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.uber.org/fx/fxtest"

	"github.com/fil-forge/piri/pkg/config/app"
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

func TestNewEchoErrorResponses(t *testing.T) {
	type request struct {
		Count int `json:"count"`
	}
	bindRequest := func(c *echo.Context) error {
		var req request
		if err := c.Bind(&req); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
	panicking := func(c *echo.Context) error { panic("secret panic details") }

	testCases := []struct {
		name         string
		handler      echo.HandlerFunc
		body         string
		expectedCode int
		expectedBody string
	}{
		{
			name:         "a handler panics",
			handler:      panicking,
			expectedCode: http.StatusInternalServerError,
			expectedBody: `{"error":"Internal Server Error"}`,
		},
		{
			name:         "a JSON field has the wrong type",
			handler:      bindRequest,
			body:         `{"count":"many"}`,
			expectedCode: http.StatusBadRequest,
			expectedBody: `{"error":"Unmarshal type error: expected=int, got=string, field=count, offset=15"}`,
		},
		{
			name:         "the JSON body is malformed",
			handler:      bindRequest,
			body:         `{count`,
			expectedCode: http.StatusBadRequest,
			expectedBody: `{"error":"Syntax error: offset=2, error=invalid character 'c' looking for beginning of object key string"}`,
		},
		{
			name:         "the JSON body is truncated",
			handler:      bindRequest,
			body:         `{"count":`,
			expectedCode: http.StatusBadRequest,
			expectedBody: `{"error":"unexpected EOF"}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEcho()
			e.POST("/test", tc.handler)
			req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(tc.body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			require.Equal(t,
				[]any{tc.expectedCode, tc.expectedBody},
				[]any{rec.Code, strings.TrimSpace(rec.Body.String())})
		})
	}
}

func TestNewEchoLogsPanicDetails(t *testing.T) {
	logs := logging.NewPipeReader()
	t.Cleanup(func() { _ = logs.Close() })
	captured := make(chan string)
	go func() {
		output, _ := io.ReadAll(logs)
		captured <- string(output)
	}()

	e := NewEcho()
	e.GET("/panic", func(c *echo.Context) error { panic("secret panic details") })
	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panic", nil))
	require.NoError(t, logs.Close())

	require.Contains(t, <-captured, "[PANIC RECOVER] secret panic details goroutine")
}

func TestEchoServerLogsThroughGoLog(t *testing.T) {
	testCases := map[string]func(s *EchoServer){
		"Echo's logger":               func(s *EchoServer) { s.echo.Logger.Warn("probe message") },
		"the HTTP server's error log": func(s *EchoServer) { s.server.ErrorLog.Print("probe message") },
	}

	originalLevel := log.Level().String()
	require.NoError(t, logging.SetLogLevel("fx/echo", "warn"))
	t.Cleanup(func() { _ = logging.SetLogLevel("fx/echo", originalLevel) })

	for name, logProbe := range testCases {
		t.Run(name, func(t *testing.T) {
			logs := logging.NewPipeReader()
			t.Cleanup(func() { _ = logs.Close() })
			captured := make(chan string)
			go func() {
				output, _ := io.ReadAll(logs)
				captured <- string(output)
			}()

			server, err := StartEchoServer(app.ServerConfig{Host: "localhost", Port: 0}, NewEcho(), fxtest.NewLifecycle(t))
			require.NoError(t, err)
			logProbe(server)
			require.NoError(t, logs.Close())

			require.Regexp(t, `"level":"warn".*"logger":"fx/echo".*"msg":"probe message"`, <-captured)
		})
	}
}

func TestNewEchoClientIP(t *testing.T) {
	testCases := []struct {
		name         string
		peerAddress  string
		forwardedFor string
		expectedIP   string
	}{
		{"a direct client without X-Forwarded-For", "203.0.113.7:4000", "", "203.0.113.7"},
		{"a direct client spoofing X-Forwarded-For", "203.0.113.7:4000", "198.51.100.1", "203.0.113.7"},
		{"a client behind a local proxy", "127.0.0.1:4000", "198.51.100.1", "198.51.100.1"},
		{"a client behind a chain of private proxies", "10.0.0.2:4000", "198.51.100.1, 10.0.0.1", "198.51.100.1"},
		{"a spoofed hop before a local proxy", "127.0.0.1:4000", "192.0.2.66, 198.51.100.1", "198.51.100.1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEcho()
			e.GET("/ip", func(c *echo.Context) error { return c.String(http.StatusOK, c.RealIP()) })
			req := httptest.NewRequest(http.MethodGet, "/ip", nil)
			req.RemoteAddr = tc.peerAddress
			if tc.forwardedFor != "" {
				req.Header.Set(echo.HeaderXForwardedFor, tc.forwardedFor)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			require.Equal(t, tc.expectedIP, rec.Body.String())
		})
	}
}
