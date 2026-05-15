package retrieval

import (
	"github.com/fil-forge/ucantone/server"
	"github.com/labstack/echo/v4"
)

// Server wraps a ucantone retrieval HTTP server for Echo route registration.
// The retrieval server uses the HTTPHeader transport codec (libforge
// ucan/retrieval) — UCAN containers ride in the X-UCAN-Container header so
// the HTTP body is free to stream blob bytes.
type Server struct {
	server *server.HTTPServer
}

func NewServer(s *server.HTTPServer) *Server {
	return &Server{server: s}
}

func (srv *Server) RegisterRoutes(e *echo.Echo) {
	e.GET("/piece/:cid", NewHandler(srv.server))
}

func NewHandler(s *server.HTTPServer) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		s.ServeHTTP(ctx.Response(), ctx.Request())
		return nil
	}
}
