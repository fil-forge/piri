package root

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"go.uber.org/fx"

	"github.com/fil-forge/libforge/identity"
	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/server"
	"github.com/fil-forge/ucantone/did/web"
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

// RegisterRoutes registers the root route and the node's DID document. The
// node is identified by the did:web of its public URL, which other services
// resolve by fetching the document from here to check what the node signs.
func (h *Handler) RegisterRoutes(e *echo.Echo) {
	e.GET("/", echo.WrapHandler(server.NewHandler(h.id)))
	e.GET(web.WellKnownDIDPath, h.didDocument)
}

func (h *Handler) didDocument(c *echo.Context) error {
	doc, err := h.id.DIDDocument()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "building DID document")
	}
	return c.JSON(http.StatusOK, doc)
}
