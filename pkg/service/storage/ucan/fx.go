package ucan

import (
	"fmt"

	ucanserver "github.com/fil-forge/go-ucanto/server"
	ucanhttp "github.com/fil-forge/go-ucanto/transport/http"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/ucan/command"
	"github.com/fil-forge/ucantone/validator/bindcap"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/validator"

	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/server/handler"
)

// Module wires the storage UCAN handler. It composes the per-ability handler
// factories (declared in this package) into a single ucanserver and exposes
// it as an echo route registrar.
var Module = fx.Module("storage/ucan",
	fx.Provide(
		NewServerHandler,
		fx.Annotate(
			AsRouteRegistrar,
			fx.ResultTags(`group:"route_registrar"`),
		),
		// Per-ability factories. Each returns a ucanserver.Option that
		// registers a handler method on the server.
		fx.Annotate(NewAccessGrantHandler, fx.ResultTags(`group:"ucan_server_handlers"`)),
		fx.Annotate(NewBlobAcceptHandler, fx.ResultTags(`group:"ucan_server_handlers"`)),
		fx.Annotate(NewBlobAllocateHandler, fx.ResultTags(`group:"ucan_server_handlers"`)),
		fx.Annotate(NewPDPInfoHandler, fx.ResultTags(`group:"ucan_server_handlers"`)),
		//fx.Annotate(NewReplicaAllocateHandler, fx.ResultTags(`group:"ucan_server_handlers"`)),
		//fx.Annotate(withReceiptLogger, fx.ResultTags(`group:"ucan_options"`)),
		// Bind types.PieceAPI to the narrow PieceResolver interface that
		// pdp/info declares as its dependency.
		func(p pdptypes.PieceAPI) PieceResolver { return p },
	),
)

// ServerHandler holds the assembled UCAN server and exposes it as an echo
// route registrar.
type ServerHandler struct {
	ucanServer *server.HTTPServer
}

type Params struct {
	fx.In

	ID       principal.Signer
	Handlers []Handler `group:"ucan_server_handlers"`
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
	seen := make(map[command.Command]struct{})

	svr := server.NewHTTP(p.ID)
	for _, h := range p.Handlers {
		// TODO(forrest)[ucan1]: nice to have duplicate detection inside the handler logic of the server.
		if _, ok := seen[h.Capability.Command()]; ok {
			return nil, fmt.Errorf("duplicate capability %q", h.Capability)
		}
		svr.Handle(h.Capability, h.Handler)
		seen[h.Capability.Command()] = struct{}{}
	}

	return &ServerHandler{svr}, nil
}

// RegisterRoutes registers the UCAN routes with Echo.
func (h *ServerHandler) RegisterRoutes(e *echo.Echo) {
	handlerFn := echo.WrapHandler(h.ucanServer)
	e.POST("/", handlerFn)
	e.POST("/piece/:cid", handlerFn)
}

// AsRouteRegistrar provides the ServerHandler as a RouteRegistrar
func AsRouteRegistrar(h *ServerHandler) echofx.RouteRegistrar {
	return h
}

// newEchoHandler wraps a ucanto server into an echo handler. The function is
// unexported because RegisterRoutes is the only caller.
func newEchoHandler(server ucanserver.ServerView[ucanserver.Service]) handler.Func {
	return func(ctx handler.Context) error {
		r := ctx.Request()
		res, err := server.Request(r.Context(), ucanhttp.NewRequest(r.Body, r.Header))
		if err != nil {
			return fmt.Errorf("handling UCAN request: %w", err)
		}

		for key, vals := range res.Headers() {
			for _, v := range vals {
				ctx.Response().Header().Add(key, v)
			}
		}

		// content type is empty as it will have been set by ucanto transport codec
		return ctx.Stream(res.Status(), "", res.Body())
	}
}

// TODO(forrest)[ucan1]: fix me!
/*
// receiptLogAllowList contains the abilities whose receipts are persisted
// to the receipt store for later retrieval.
var receiptLogAllowList = []string{
	blob.AllocateAbility,
	blob.AcceptAbility,
	replica.AllocateAbility,
}

// withReceiptLogger stores important receipts that we may need to access in
// the future.
func withReceiptLogger(store receiptstore.ReceiptStore) ucanserver.Option {
	return ucanserver.WithReceiptLogger(func(ctx context.Context, rcpt *receipt.Receipt, inv invocation.Invocation) error {
		if len(inv.Capabilities()) != 1 {
			log.Warn("Expected exactly one capability in invocation")
			return nil
		}
		capability := inv.Capabilities()[0]
		if !slices.Contains(receiptLogAllowList, capability.Can()) {
			log.Info("Receipt is for a %s invocation, ignoring", capability.Can())
			return nil
		}
		// Make sure the receipt is self-contained, i.e. it also has invocation blocks
		fullRcpt, err := rcpt.Clone()
		if err != nil {
			return err
		}
		if err := fullRcpt.AttachInvocation(inv); err != nil {
			return err
		}
		if err := store.Put(ctx, fullRcpt); err != nil {
			log.Errorw("putting receipt to store", "error", err)
			return err
		}
		return nil
	})
}
*/
