package storage

import (
	"github.com/fil-forge/ucantone/server"
	"github.com/labstack/echo/v4"

	"github.com/fil-forge/piri/pkg/server/handler"
)

type Server struct {
	ucanServer *server.HTTPServer
}

// NewServer wraps a ucantone HTTP UCAN server for Echo route registration.
func NewServer(s *server.HTTPServer) *Server {
	return &Server{ucanServer: s}
}

func (srv *Server) RegisterRoutes(e *echo.Echo) {
	h := NewHandler(srv.ucanServer).ToEcho()
	e.POST("/", h)
	e.POST("/piece/:cid", h)
}

// NewHandler returns a piri handler.Func that dispatches incoming HTTP UCAN
// requests to the underlying ucantone server.
func NewHandler(s *server.HTTPServer) handler.Func {
	return func(ctx handler.Context) error {
		s.ServeHTTP(ctx.Response(), ctx.Request())
		return nil
	}
}
