package claims

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"

	"github.com/fil-forge/go-ucanto/core/car"

	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/server/handler"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/delegationstore"
)

var _ echofx.RouteRegistrar = (*Server)(nil)

type Server struct {
	claims delegationstore.DelegationStore
}

func NewServer(claims delegationstore.DelegationStore) (*Server, error) {
	return &Server{claims}, nil
}

func (srv *Server) RegisterRoutes(e *echo.Echo) {
	e.GET("/claim/:claim", NewHandler(srv.claims).ToEcho())
}

func NewHandler(claims delegationstore.DelegationStore) handler.Func {
	return func(ctx handler.Context) error {
		r := ctx.Request()
		parts := strings.Split(r.URL.Path, "/")
		c, err := cid.Parse(parts[len(parts)-1])
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Errorf("invalid claim CID: %w", err))
		}

		dlg, err := claims.Get(r.Context(), c)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return echo.NewHTTPError(http.StatusNotFound, fmt.Errorf("not found: %s", c))
			}
			return fmt.Errorf("failed to get claim: %w", err)
		}

		return ctx.Stream(http.StatusOK, car.ContentType, bytes.NewReader(dlg.Bytes()))
	}
}
