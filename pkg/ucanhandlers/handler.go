package ucanhandlers

import (
	"fmt"

	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/libforge/ucan/retrieval"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
)

// Group tag strings used by per-capability fx.Provide registrations and
// by the params structs below. Kept paired so handlers register on the
// same server they're collected for. Struct tags can't reference a
// constant value at compile time, so the literal strings appear in both
// the tag and the helper below — keep them in sync.
const (
	RPCHandlersGroupTag       = `group:"ucan_rpc_handlers"`
	RetrievalHandlersGroupTag = `group:"ucan_retrieval_handlers"`
	RPCOptionsGroupTag        = `group:"ucan_rpc_options"`
	RetrievalOptionsGroupTag  = `group:"ucan_retrieval_options"`
)

// ProvideRPC annotates a per-capability handler constructor so that fx
// collects it into the body-CAR RPC server's handler group. Per-capability
// fx.Module declarations use this rather than spelling out fx.Annotate +
// fx.ResultTags, both to compress boilerplate and to keep the group tag
// in a single place.
func ProvideRPC(ctor any) any {
	return fx.Annotate(ctor, fx.ResultTags(RPCHandlersGroupTag))
}

// ProvideRetrieval is the byte-streaming counterpart to [ProvideRPC].
func ProvideRetrieval(ctor any) any {
	return fx.Annotate(ctor, fx.ResultTags(RetrievalHandlersGroupTag))
}

// ProvideRPCOption annotates a constructor whose result is a single
// server.HTTPOption so fx collects it into the RPC server's options
// group. The constructor may be parameterless (returns a constant
// option) or may depend on other fx-managed values:
//
//	ucanhandlers.ProvideRPCOption(func() server.HTTPOption {
//	    return server.WithReceiptTimestamps(true)
//	})
//
//	ucanhandlers.ProvideRPCOption(func(l EventListener) server.HTTPOption {
//	    return server.WithEventListener(l)
//	})
//
// Each provider contributes exactly one option; call ProvideRPCOption
// multiple times to register multiple options.
func ProvideRPCOption(ctor any) any {
	return fx.Annotate(ctor, fx.ResultTags(RPCOptionsGroupTag))
}

// ProvideRetrievalOption is the byte-streaming counterpart to [ProvideRPCOption].
func ProvideRetrievalOption(ctor any) any {
	return fx.Annotate(ctor, fx.ResultTags(RetrievalOptionsGroupTag))
}

// RPCParams collects the handlers registered on the body-CAR UCAN server
// (server.NewHTTP). These handle invocations whose response is itself a
// UCAN container of receipts (e.g. blob/accept, access/grant, pdp/info).
type RPCParams struct {
	fx.In

	ID       identity.Identity
	Handlers []server.Route      `group:"ucan_rpc_handlers"`
	Options  []server.HTTPOption `group:"ucan_rpc_options"`
}

// RetrievalParams collects the handlers registered on the header-container
// UCAN server (retrieval.NewServer). These handle invocations whose
// response body is a raw byte stream (e.g. blob/retrieve, content/retrieve).
type RetrievalParams struct {
	fx.In

	ID       identity.Identity
	Handlers []server.Route      `group:"ucan_retrieval_handlers"`
	Options  []server.HTTPOption `group:"ucan_retrieval_options"`
}

func NewRPC(p RPCParams) (*RPCHandler, error) {
	svr := server.NewHTTP(p.ID, p.Options...)
	if err := register(svr, p.Handlers); err != nil {
		return nil, err
	}
	return &RPCHandler{svr: svr}, nil
}

func NewRetrieval(p RetrievalParams) (*RetrievalHandler, error) {
	svr := retrieval.NewServer(p.ID, p.Options...)
	if err := register(svr, p.Handlers); err != nil {
		return nil, err
	}
	return &RetrievalHandler{svr: svr}, nil
}

// handleRegistrar is satisfied by both *server.HTTPServer and *retrieval.Server.
type handleRegistrar interface {
	Handle(ucan.Command, execution.HandlerFunc)
}

func register(svr handleRegistrar, handlers []server.Route) error {
	seen := make(map[ucan.Command]struct{})
	for _, h := range handlers {
		// TODO(forrest)[ucan1]: nice to have duplicate detection inside the handler logic of the server.
		if _, ok := seen[h.Command]; ok {
			return fmt.Errorf("duplicate capability %q", h.Command)
		}
		svr.Handle(h.Command, h.Handler)
		seen[h.Command] = struct{}{}
	}
	return nil
}

type RPCHandler struct {
	svr *server.HTTPServer
}

func (h *RPCHandler) RegisterRoutes(e *echo.Echo) {
	rpc := echo.WrapHandler(h.svr)
	e.POST("/", rpc)
	e.POST("/piece/:cid", rpc)
}

type RetrievalHandler struct {
	svr *retrieval.Server
}

func (h *RetrievalHandler) RegisterRoutes(e *echo.Echo) {
	e.GET("/piece/:cid", echo.WrapHandler(h.svr))
}
