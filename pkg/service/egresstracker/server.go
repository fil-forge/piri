package egresstracker

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"

	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/local/retrievaljournal"
)

var _ echofx.RouteRegistrar = (*Server)(nil)

const (
	ReceiptsPath = "/receipts"
	// carContentType is the IANA-registered media type for IPLD CAR files.
	// The egress journal stores receipts as a multi-block CAR; consumers
	// fetch the CAR and decode it themselves.
	carContentType = "application/vnd.ipld.car"
)

type Server struct {
	egressBatches retrievaljournal.Journal
}

func NewServer(egressBatches retrievaljournal.Journal) (*Server, error) {
	return &Server{egressBatches}, nil
}

func (srv *Server) RegisterRoutes(e *echo.Echo) {
	e.GET(ReceiptsPath+"/:cid", NewHandler(srv.egressBatches))
}

func NewHandler(egressBatches retrievaljournal.Journal) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		cid, err := cid.Parse(ctx.Param("cid"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Errorf("invalid batch CID: %w", err))
		}

		batch, err := egressBatches.GetBatch(ctx.Request().Context(), cid)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return echo.NewHTTPError(http.StatusNotFound, fmt.Errorf("batch not found: %s", cid))
			}
			return fmt.Errorf("failed to get batch from store: %w", err)
		}
		defer batch.Close()

		return ctx.Stream(http.StatusOK, carContentType, batch)
	}
}
