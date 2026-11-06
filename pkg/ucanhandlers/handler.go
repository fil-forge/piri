package ucanhandlers

import (
	"fmt"

	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/libforge/ucan/retrieval"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/server/middleware"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
)

// Group tag strings used by per-capability fx.Provide registrations and by the
// params structs below. Kept paired so handlers register on the same server
// they're collected for. A capability registers with fx.ResultTags and one of
// these, and which one it picks decides the authorization its route is served
// behind (see NewRPC and NewRetrieval). Struct tags can't reference a constant
// value at compile time, so the literal strings appear in both the tag and the
// constant — keep them in sync.
const (
	RPCHandlersGroupTag            = `group:"ucan_rpc_handlers"`
	RPCOpenHandlersGroupTag        = `group:"ucan_rpc_open_handlers"`
	RetrievalHandlersGroupTag      = `group:"ucan_retrieval_handlers"`
	RetrievalSpaceHandlersGroupTag = `group:"ucan_retrieval_space_handlers"`
	RPCOptionsGroupTag             = `group:"ucan_rpc_options"`
	RetrievalOptionsGroupTag       = `group:"ucan_retrieval_options"`
)

// RPCParams collects the handlers registered on the body-CAR UCAN server
// (server.NewHTTP). These handle invocations whose response is itself a
// UCAN container of receipts (e.g. blob/accept, access/grant, pdp/info).
type RPCParams struct {
	fx.In

	ID identity.Identity
	// Handlers are served behind the subject checks: subjected to this node and
	// issued by someone holding a delegation from it.
	Handlers []server.Route `group:"ucan_rpc_handlers"`
	// OpenHandlers are served as they are: a capability invoked self-signed, or
	// over a subject other than this node.
	OpenHandlers []server.Route      `group:"ucan_rpc_open_handlers"`
	Options      []server.HTTPOption `group:"ucan_rpc_options"`
}

// RetrievalParams collects the handlers registered on the header-container
// UCAN server (retrieval.NewServer). These handle invocations whose
// response body is a raw byte stream (e.g. blob/retrieve, content/retrieve).
type RetrievalParams struct {
	fx.In

	ID identity.Identity
	// Handlers are subjected to this node.
	Handlers []server.Route `group:"ucan_retrieval_handlers"`
	// SpaceHandlers are subjected to a space rather than to this node.
	SpaceHandlers []server.Route      `group:"ucan_retrieval_space_handlers"`
	Options       []server.HTTPOption `group:"ucan_retrieval_options"`
}

func NewRPC(p RPCParams) (*RPCHandler, error) {
	svr := server.NewHTTP(p.ID, p.Options...)
	// A self-signed invocation carries the subject's whole authority with no
	// proofs, so a capability this node serves for others requires one subjected
	// to the node and issued by someone it delegated to.
	guarded := middleware.Apply(p.Handlers,
		middleware.NotSelfSigned(),
		middleware.OnlySubject(p.ID.DID()),
	)
	if err := register(svr, append(guarded, p.OpenHandlers...)); err != nil {
		return nil, err
	}
	return &RPCHandler{svr: svr}, nil
}

func NewRetrieval(p RetrievalParams) (*RetrievalHandler, error) {
	svr := retrieval.NewServer(p.ID, p.Options...)
	// Retrieval from the node's own authority is pinned to the node; a space's
	// own retrieval is not, since the space is the subject there and its key
	// holder may invoke over it directly.
	guarded := middleware.Apply(p.Handlers, middleware.OnlySubject(p.ID.DID()))
	if err := register(svr, append(guarded, p.SpaceHandlers...)); err != nil {
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
