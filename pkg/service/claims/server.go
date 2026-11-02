package claims

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"

	"github.com/fil-forge/ucantone/ipld/codec/dagcbor"
	"github.com/fil-forge/ucantone/ucan/invocation"

	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/server/handler"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
)

var _ echofx.RouteRegistrar = (*Server)(nil)

type Server struct {
	claims invocationstore.InvocationStore
}

func NewServer(claims invocationstore.InvocationStore) (*Server, error) {
	return &Server{claims}, nil
}

func (srv *Server) RegisterRoutes(e *echo.Echo) {
	e.GET("/claim/:claim", NewHandler(srv.claims).ToEcho())
}

func NewHandler(claims invocationstore.InvocationStore) handler.Func {
	return func(ctx handler.Context) error {
		r := ctx.Request()
		parts := strings.Split(r.URL.Path, "/")
		c, err := cid.Parse(parts[len(parts)-1])
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Errorf("invalid claim CID: %w", err))
		}

		claim, err := claims.Get(r.Context(), c)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return echo.NewHTTPError(http.StatusNotFound, fmt.Errorf("not found: %s", c))
			}
			return fmt.Errorf("failed to get claim: %w", err)
		}

		b, err := invocation.Encode(claim)
		if err != nil {
			return fmt.Errorf("failed to encode claim: %w", err)
		}

		return ctx.Stream(http.StatusOK, dagcbor.ContentType, bytes.NewReader(b))
	}
}
