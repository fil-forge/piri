package root

import (
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"github.com/fil-forge/libforge/identity"
	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/server"
)

// Module provides the root handler with route registrar tag
var Module = fx.Module("root-handler",
	fx.Provide(
		fx.Annotate(
			NewRootHandler,
			fx.As(new(echofx.RouteRegistrar)),
			fx.ResultTags(`group:"route_registrar"`),
		),
	),
)

var _ echofx.RouteRegistrar = (*Handler)(nil)

// Handler provides the root route handler
type Handler struct {
	id identity.Identity
}

// NewRootHandler creates a new root handler
func NewRootHandler(id identity.Identity) *Handler {
	return &Handler{id: id}
}

// RegisterRoutes registers the root route
func (h *Handler) RegisterRoutes(e *echo.Echo) {
	e.GET("/", echo.WrapHandler(server.NewHandler(h.id)))
}
