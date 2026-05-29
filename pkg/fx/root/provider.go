package root

import (
	"github.com/fil-forge/ucantone/principal"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

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
	id principal.Signer
}

// NewRootHandler creates a new root handler
func NewRootHandler(id principal.Signer) *Handler {
	return &Handler{id: id}
}

// RegisterRoutes registers the root route
func (h *Handler) RegisterRoutes(e *echo.Echo) {
	e.GET("/", echo.WrapHandler(server.NewHandler(h.id)))
}
