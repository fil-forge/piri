package ucan

import (
	ucanserver "github.com/fil-forge/ucantone/server"
	logging "github.com/ipfs/go-log/v2"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/fx/storage/ucan/handlers"
	"github.com/fil-forge/piri/pkg/service/storage"
	ucanhandlers "github.com/fil-forge/piri/pkg/service/storage/ucan"
	"github.com/fil-forge/ucantone/principal"
)

var log = logging.Logger("fx/storage/ucan")

type Handler struct {
	ucanServer *ucanserver.HTTPServer
}

var Module = fx.Module("storage/ucan/server",
	fx.Provide(
		NewHandler,
		fx.Annotate(
			AsRouteRegistrar,
			fx.ResultTags(`group:"route_registrar"`),
		),
		fx.Annotate(
			ProvideServer,
			fx.ResultTags(`name:"storage_ucan_server"`),
		),
	),
	handlers.Module,
)

type Params struct {
	fx.In

	ID       principal.Signer
	Handlers []ucanhandlers.Handler `group:"ucan_handlers"`
	Options  []ucanserver.HTTPOption `group:"ucan_options"`
}

// NewHandler builds the storage node's UCAN HTTP server and registers each
// fx-collected handler on it.
func NewHandler(p Params) *Handler {
	srv := ucanserver.NewHTTP(p.ID, p.Options...)
	for _, h := range p.Handlers {
		srv.Handle(h.Capability, h.Handler)
		log.Infow("registered UCAN handler", "command", h.Capability.Command())
	}
	return &Handler{ucanServer: srv}
}

// RegisterRoutes registers the UCAN routes with Echo
func (h *Handler) RegisterRoutes(e *echo.Echo) {
	handler := storage.NewHandler(h.ucanServer).ToEcho()
	e.POST("/", handler)
	e.POST("/piece/:cid", handler)
}

// AsRouteRegistrar provides the Handler as a RouteRegistrar
func AsRouteRegistrar(h *Handler) echofx.RouteRegistrar {
	return h
}

// ProvideServer provides the UCAN server for tests / integration.
func ProvideServer(h *Handler) *ucanserver.HTTPServer {
	return h.ucanServer
}
