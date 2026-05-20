package ucan

import (
	"context"
	"fmt"
	"slices"

	"github.com/fil-forge/go-libstoracha/capabilities/blob"
	"github.com/fil-forge/go-libstoracha/capabilities/blob/replica"
	"github.com/fil-forge/go-ucanto/core/invocation"
	"github.com/fil-forge/go-ucanto/core/receipt"
	"github.com/fil-forge/go-ucanto/principal"
	ucanserver "github.com/fil-forge/go-ucanto/server"
	ucanhttp "github.com/fil-forge/go-ucanto/transport/http"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/server/handler"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
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
		ProvideServerView,
		// Per-ability factories. Each returns a ucanserver.Option that
		// registers a handler method on the server.
		fx.Annotate(WithAccessGrantMethod, fx.ResultTags(`group:"ucan_options"`)),
		fx.Annotate(WithBlobAllocateMethod, fx.ResultTags(`group:"ucan_options"`)),
		fx.Annotate(WithBlobAcceptMethod, fx.ResultTags(`group:"ucan_options"`)),
		fx.Annotate(WithPDPInfoMethod, fx.ResultTags(`group:"ucan_options"`)),
		fx.Annotate(WithReplicaAllocateMethod, fx.ResultTags(`group:"ucan_options"`)),
		fx.Annotate(withReceiptLogger, fx.ResultTags(`group:"ucan_options"`)),
		// Bind types.PieceAPI to the narrow PieceResolver interface that
		// pdp/info declares as its dependency.
		func(p pdptypes.PieceAPI) PieceResolver { return p },
	),
)

// ServerHandler holds the assembled UCAN server and exposes it as an echo
// route registrar.
type ServerHandler struct {
	ucanServer ucanserver.ServerView[ucanserver.Service]
}

type Params struct {
	fx.In

	ID      principal.Signer
	Options []ucanserver.Option `group:"ucan_options"`
}

func NewServerHandler(p Params) (*ServerHandler, error) {
	options := []ucanserver.Option{
		ucanserver.WithErrorHandler(func(err ucanserver.HandlerExecutionError[any]) {
			l := log.With("error", err.Error())
			if s := err.Stack(); s != "" {
				l = l.With("stack", s)
			}
			l.Error("ucan storage handler execution error")
		}),
	}
	options = append(options, p.Options...)
	ucanSvr, err := ucanserver.NewServer(p.ID, options...)
	if err != nil {
		return nil, fmt.Errorf("creating ucan server: %w", err)
	}

	return &ServerHandler{ucanSvr}, nil
}

// RegisterRoutes registers the UCAN routes with Echo.
func (h *ServerHandler) RegisterRoutes(e *echo.Echo) {
	handlerFn := newEchoHandler(h.ucanServer).ToEcho()
	e.POST("/", handlerFn)
	e.POST("/piece/:cid", handlerFn)
}

// AsRouteRegistrar provides the ServerHandler as a RouteRegistrar
func AsRouteRegistrar(h *ServerHandler) echofx.RouteRegistrar {
	return h
}

// ProvideServerView provides the UCAN ServerView for testing.
func ProvideServerView(h *ServerHandler) ucanserver.ServerView[ucanserver.Service] {
	return h.ucanServer
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
	return ucanserver.WithReceiptLogger(func(ctx context.Context, rcpt receipt.AnyReceipt, inv invocation.Invocation) error {
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
