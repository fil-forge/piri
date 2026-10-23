package echo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	logging "github.com/ipfs/go-log/v2"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"go.uber.org/fx"

	echootel "github.com/labstack/echo-otel/v5"
	"go.opentelemetry.io/otel"

	"github.com/fil-forge/piri/pkg/config/app"
	pirimiddleware "github.com/fil-forge/piri/pkg/pdp/httpapi/server/middleware"
)

var log = logging.Logger("fx/echo")

var Module = fx.Module("echo",
	fx.Provide(
		NewEcho,
	),
	fx.Invoke(
		RegisterRoutes,
		StartEchoServer,
	),
)

// RouteRegistrar defines the interface for services that register Echo routes
type RouteRegistrar interface {
	RegisterRoutes(e *echo.Echo)
}

// NewEcho creates a new Echo instance with default middleware
func NewEcho() *echo.Echo {
	e := echo.New()
	e.Logger = echoLogger()
	e.JSONSerializer = jsonSerializer{}
	// Behind a reverse proxy, such as the nginx in deploy/full-node, the peer
	// address is the proxy's. Take the client address from X-Forwarded-For,
	// trusting only the hops on loopback, link-local and private networks, so
	// that a client connecting directly cannot spoof its address.
	e.IPExtractor = echo.ExtractIPFromXFFHeader()

	// Set custom error handler
	e.HTTPErrorHandler = pirimiddleware.CustomHTTPErrorHandler

	// Add default middleware
	e.Use(pirimiddleware.RequestLogger(log))
	e.Use(middleware.Recover())
	// Custom middlewares
	e.Use(ErrorLogger(log))
	e.Use(echootel.NewMiddlewareWithConfig(echootel.Config{
		ServerName:    "piri",
		Propagators:   otel.GetTextMapPropagator(),
		MeterProvider: otel.GetMeterProvider(),
		Skipper:       skipHealthChecks,
		// Send the error response before echootel records the request, so that
		// spans, HTTP server metrics and the request log see the status code
		// CustomHTTPErrorHandler sends. Otherwise every error that carries no
		// status code, like a types.Error of KindNotFound, is recorded as 500.
		OnNextError: func(c *echo.Context, err error) {
			c.Echo().HTTPErrorHandler(c, err)
		},
	}))

	return e
}

// echoLogger sends the messages Echo logs itself through go-log, under the
// same subsystem as this package.
func echoLogger() *slog.Logger {
	return slog.New(logging.SlogHandler()).With("logger", "fx/echo")
}

// healthCheckPaths are the routes pkg/health registers. Container
// healthchecks poll them every few seconds, and each request would otherwise
// start a trace of its own and bury the traces worth reading.
var healthCheckPaths = map[string]struct{}{
	"/healthz": {},
	"/livez":   {},
	"/readyz":  {},
}

// skipHealthChecks keeps health checks out of echootel's traces and HTTP
// server metrics.
func skipHealthChecks(c *echo.Context) bool {
	_, ok := healthCheckPaths[c.Request().URL.Path]
	return ok
}

// EchoServer wraps Echo with fx lifecycle management
type EchoServer struct {
	echo   *echo.Echo
	server *http.Server
	addr   string
}

// StartEchoServer runs a Echo server with lifecycle management
func StartEchoServer(cfg app.ServerConfig, e *echo.Echo, lc fx.Lifecycle) (*EchoServer, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	httpServer := &http.Server{
		Addr:     addr,
		Handler:  e,
		ErrorLog: slog.NewLogLogger(e.Logger.Handler(), slog.LevelWarn),
	}
	server := &EchoServer{
		echo:   e,
		server: httpServer,
		addr:   addr,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Infof("Starting Echo server on %s", addr)

			// Start server in a goroutine
			go func() {
				if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Errorf("Echo server error: %v", err)
				}
			}()

			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("Shutting down Echo server")
			defer log.Info("Echo server stopped")
			// Per go docs on this method:
			// Shutdown gracefully shuts down the server without interrupting any
			// active connections. Shutdown works by first closing all open
			// listeners, then closing all idle connections, and then waiting
			// indefinitely for connections to return to idle and then shut down.
			// If the provided context expires before the shutdown is complete,
			// Shutdown returns the context's error, otherwise it returns any
			// error returned from closing the [Server]'s underlying Listener(s).
			//
			// When Shutdown is called, [Serve], [ListenAndServe], and
			// [ListenAndServeTLS] immediately return [ErrServerClosed].
			//
			// The timeout of the context passed to this method is configured
			// by setting fx.StopTimeout([duration]),
			return httpServer.Shutdown(ctx)
		},
	})

	return server, nil
}

// RouteParams collects all route registrars
type RouteParams struct {
	fx.In

	Registrars []RouteRegistrar `group:"route_registrar"`
}

// RegisterRoutes registers all routes from collected registrars
func RegisterRoutes(e *echo.Echo, params RouteParams) {
	log.Infof("Registering routes from %d registrars", len(params.Registrars))

	for _, registrar := range params.Registrars {
		registrar.RegisterRoutes(e)
	}
}

// Address returns the server's listening address
func (s *EchoServer) Address() string {
	return s.addr
}
