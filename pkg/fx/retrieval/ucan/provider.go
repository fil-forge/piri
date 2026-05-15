package ucan

import (
	"github.com/fil-forge/libforge/ucan/retrieval"
	"github.com/fil-forge/ucantone/principal"
	ucanserver "github.com/fil-forge/ucantone/server"
	logging "github.com/ipfs/go-log/v2"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/fx/retrieval/ucan/handlers"
	retrievalsvc "github.com/fil-forge/piri/pkg/service/retrieval"
	ucanhandlers "github.com/fil-forge/piri/pkg/service/retrieval/ucan"
)

var log = logging.Logger("fx/retrieval/ucan")

type Handler struct {
	ucanServer *ucanserver.HTTPServer
}

var Module = fx.Module("retrieval/ucan/server",
	fx.Provide(
		NewHandler,
		fx.Annotate(
			AsRouteRegistrar,
			fx.ResultTags(`group:"route_registrar"`),
		),
		fx.Annotate(
			ProvideServer,
			fx.ResultTags(`name:"retrieval_ucan_server"`),
		),
	),
	handlers.Module,
)

type Params struct {
	fx.In

	ID       principal.Signer
	Handlers []ucanhandlers.Handler  `group:"retrieval_ucan_handlers"`
	Options  []ucanserver.HTTPOption `group:"retrieval_ucan_options"`
}

// NewHandler builds the retrieval HTTP UCAN server (HTTPHeader codec) and
// registers each fx-collected handler on it.
func NewHandler(p Params) *Handler {
	srv := retrieval.NewServer(p.ID, p.Options...)
	for _, h := range p.Handlers {
		srv.Handle(h.Capability, h.Handler)
		log.Infow("registered retrieval UCAN handler", "command", h.Capability.Command())
	}
	return &Handler{ucanServer: srv}
}

// RegisterRoutes registers the UCAN routes with Echo.
func (h *Handler) RegisterRoutes(e *echo.Echo) {
	e.GET("/piece/:cid", retrievalsvc.NewHandler(h.ucanServer))
}

// AsRouteRegistrar provides the Handler as a RouteRegistrar.
func AsRouteRegistrar(h *Handler) echofx.RouteRegistrar {
	return h
}

// ProvideServer provides the UCAN retrieval server for tests / integration.
func ProvideServer(h *Handler) *ucanserver.HTTPServer {
	return h.ucanServer
}
