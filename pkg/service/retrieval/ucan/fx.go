package ucan

import (
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"github.com/fil-forge/libforge/ucan/retrieval"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/validator"
	"github.com/fil-forge/ucantone/validator/bindcap"

	"github.com/fil-forge/piri/pkg/config/app"
	echofx "github.com/fil-forge/piri/pkg/fx/echo"
)

// Module wires the retrieval UCAN handler.
var Module = fx.Module("retrieval/ucan",
	fx.Provide(
		NewServerHandler,
		fx.Annotate(
			AsRouteRegistrar,
			fx.ResultTags(`group:"route_registrar"`),
		),
		fx.Annotate(NewBlobRetrieveHandler, fx.ResultTags(`group:"ucan_retrieval_server_handlers"`)),
		fx.Annotate(NewSpaceContentRetrieveHandler, fx.ResultTags(`group:"ucan_retrieval_server_handlers"`)),
		//fx.Annotate(withReceiptLogger, fx.ResultTags(`group:"ucan_retrieval_options"`)),
	),
)

// ServerHandler holds the assembled UCAN retrieval server and exposes it as
// an echo route registrar.
type ServerHandler struct {
	ucanServer *retrieval.Server
}

type Params struct {
	fx.In

	ID      principal.Signer
	Upload  app.UploadServiceConfig
	Options []server.HTTPOption `group:"ucan_retrieval_options"`
}

// Handler pairs a UCAN capability with its execution handler. Per-capability
// providers return one; NewServerHandler registers them all.
type Handler struct {
	Capability validator.Capability
	Handler    execution.HandlerFunc
}

// TypedHandler ties the capability's bound argument type to the handler's
// argument type at compile time. Per-capability factories use this rather
// than constructing a raw Handler{} literal so that
//
//	TypedHandler(access.Grant, func(
//		req *bindexec.Request[*blob.AllocateArguments],   // wrong type
//		res *bindexec.Response[*access.GrantOK],
//	) error { ... })
//
// fails to compile rather than failing at runtime with a
// MalformedArgumentsError. Equivalent to [server.HandleTyped] but
// produces a Handler value for the fx group instead of registering
// directly on a server.
func TypedHandler[A bindcap.Arguments, O bindexec.Success](
	capability *bindcap.Capability[A],
	fn bindexec.HandlerFunc[A, O],
) Handler {
	return Handler{
		Capability: capability,
		Handler:    bindexec.NewHandler(fn),
	}
}

func NewServerHandler(p Params) (*ServerHandler, error) {
	svr := retrieval.NewServer(p.ID)
	return &ServerHandler{svr}, nil
}

// RegisterRoutes registers the UCAN routes with Echo.
func (h *ServerHandler) RegisterRoutes(e *echo.Echo) {
	e.GET("/piece/:cid", echo.WrapHandler(h.ucanServer))
}

// AsRouteRegistrar provides the ServerHandler as a RouteRegistrar.
func AsRouteRegistrar(h *ServerHandler) echofx.RouteRegistrar {
	return h
}

// TODO(forrest)[ucan1]: fix me!
/*

type retrievalJournalEventListener struct {
	ets *egresstracker.Service
}

var _ server.ResponseEncodeListener = (*retrievalJournalEventListener)(nil)

func (r retrievalJournalEventListener) OnResponseEncode(ctx context.Context, container ucan.Container) error {
	if r.ets == nil {
		return nil
	}
	// Filter out capabilities that are not space/content/retrieve
	if len(container.Invocations()) != 1 {
		log.Debug("Expected exactly one capability in invocation")
		return nil
	}

	cmd := container.Invocations()[0].Command()

	if cmd != content.RetrieveCommand {
		log.Debug("Receipt is for a %s invocation, ignoring", cmd)
		return nil

	}

	return r.ets.AddReceipt(context.TODO(), container.Receipts()[0])
}

func withReceiptLogger(ets *egresstracker.Service) server.HTTPOption {
	return server.WithEventListener(retrievalJournalEventListener{ets: ets})
}
*/
