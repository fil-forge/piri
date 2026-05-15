package ucan

import (
	"context"
	"fmt"

	ucancap "github.com/fil-forge/go-libstoracha/capabilities/ucan"
	"github.com/fil-forge/go-libstoracha/capabilities/space/content"
	"github.com/fil-forge/go-libstoracha/failure"
	"github.com/fil-forge/go-ucanto/core/delegation"
	"github.com/fil-forge/go-ucanto/core/invocation"
	"github.com/fil-forge/go-ucanto/core/receipt"
	"github.com/fil-forge/go-ucanto/principal"
	ucanserver "github.com/fil-forge/go-ucanto/server"
	ucanretrieval "github.com/fil-forge/go-ucanto/server/retrieval"
	ucanhttp "github.com/fil-forge/go-ucanto/transport/http"
	"github.com/fil-forge/go-ucanto/ucan"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/service/egresstracker"
)

// Module wires the retrieval UCAN handler.
var Module = fx.Module("retrieval/ucan",
	fx.Provide(
		NewServerHandler,
		fx.Annotate(
			AsRouteRegistrar,
			fx.ResultTags(`group:"route_registrar"`),
		),
		ProvideServerView,
		fx.Annotate(WithBlobRetrieveMethod, fx.ResultTags(`group:"ucan_retrieval_options"`)),
		fx.Annotate(WithSpaceContentRetrieveMethod, fx.ResultTags(`group:"ucan_retrieval_options"`)),
		fx.Annotate(withErrorHandler, fx.ResultTags(`group:"ucan_retrieval_options"`)),
		fx.Annotate(withReceiptLogger, fx.ResultTags(`group:"ucan_retrieval_options"`)),
	),
)

// ServerHandler holds the assembled UCAN retrieval server and exposes it as
// an echo route registrar.
type ServerHandler struct {
	ucanServer ucanserver.ServerView[ucanretrieval.Service]
}

type Params struct {
	fx.In

	ID      principal.Signer
	Config  app.AppConfig
	Options []ucanretrieval.Option `group:"ucan_retrieval_options"`
}

func NewServerHandler(p Params) (*ServerHandler, error) {
	// Create a local delegation to the upload service that allows it to issue
	// attestations. When the validator sees this delegation, it will accept
	// attestations issued by the upload service.
	attestDlg, err := delegation.Delegate(
		p.ID,
		p.Config.UCANService.Services.Upload.Connection.ID(),
		[]ucan.Capability[ucan.NoCaveats]{
			ucan.NewCapability(
				ucancap.AttestAbility,
				p.ID.DID().String(),
				ucan.NoCaveats{},
			),
		},
		delegation.WithNoExpiration(),
	)
	if err != nil {
		return nil, err
	}

	options := []ucanretrieval.Option{
		ucanretrieval.WithAuthorityProofs(attestDlg),
	}
	options = append(options, p.Options...)
	ucanSvr, err := ucanretrieval.NewServer(p.ID, options...)
	if err != nil {
		return nil, fmt.Errorf("creating ucan retrieval server: %w", err)
	}

	return &ServerHandler{ucanSvr}, nil
}

// RegisterRoutes registers the UCAN routes with Echo.
func (h *ServerHandler) RegisterRoutes(e *echo.Echo) {
	e.GET("/piece/:cid", newEchoHandler(h.ucanServer))
}

// AsRouteRegistrar provides the ServerHandler as a RouteRegistrar.
func AsRouteRegistrar(h *ServerHandler) echofx.RouteRegistrar {
	return h
}

// ProvideServerView provides the UCAN ServerView for testing.
func ProvideServerView(h *ServerHandler) ucanserver.ServerView[ucanretrieval.Service] {
	return h.ucanServer
}

// newEchoHandler wraps a ucanto retrieval server into an echo handler.
func newEchoHandler(server ucanserver.ServerView[ucanretrieval.Service]) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		r := ctx.Request()
		res, err := server.Request(r.Context(), ucanhttp.NewInboundRequest(r.URL, r.Body, r.Header))
		if err != nil {
			return fmt.Errorf("handling UCAN retrieval request: %w", err)
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

func withErrorHandler() ucanretrieval.Option {
	return ucanretrieval.WithErrorHandler(func(err ucanserver.HandlerExecutionError[any]) {
		l := log.With("error", err.Error())
		if s := err.Stack(); s != "" {
			l = l.With("stack", s)
		}
		l.Error("ucan retrieval handler execution error")
	})
}

func withReceiptLogger(ets *egresstracker.Service) ucanretrieval.Option {
	return ucanretrieval.WithReceiptLogger(func(_ context.Context, rcpt receipt.AnyReceipt, inv invocation.Invocation) error {
		// Filter out capabilities that are not space/content/retrieve
		if len(inv.Capabilities()) != 1 {
			log.Warn("Expected exactly one capability in invocation")
			return nil
		}

		capability := inv.Capabilities()[0]
		if capability.Can() != content.RetrieveAbility {
			log.Info("Receipt is for a %s invocation, ignoring", capability.Can())
			return nil
		}

		// Egress tracking is optional, the service will be nil if it is disabled
		if ets == nil {
			log.Warn("Egress tracking is not configured")
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

		retrievalRcpt, err := receipt.Rebind[content.RetrieveOk, failure.FailureModel](fullRcpt, content.RetrieveOkType(), failure.FailureType())
		if err != nil {
			return err
		}

		if err := ets.AddReceipt(context.Background(), retrievalRcpt); err != nil {
			return err
		}

		return nil
	})
}
