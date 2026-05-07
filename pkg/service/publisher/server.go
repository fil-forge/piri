package publisher

import (
	"fmt"

	"github.com/fil-forge/go-libstoracha/ipnipublisher/server"
	"github.com/fil-forge/go-libstoracha/ipnipublisher/store"
	"github.com/labstack/echo/v4"

	echofx "github.com/fil-forge/piri/pkg/fx/echo"
)

var _ echofx.RouteRegistrar = (*Server)(nil)

type Server struct {
	server *server.Server
}

func NewServer(store store.EncodeableStore) (*Server, error) {
	server, err := server.NewServer(store)
	if err != nil {
		return nil, err
	}
	return &Server{server}, nil
}

func (srv *Server) RegisterRoutes(e *echo.Echo) {
	route := fmt.Sprintf("%s/:ad", server.IPNIPath)
	e.GET(route, echo.WrapHandler(srv.server))
}
