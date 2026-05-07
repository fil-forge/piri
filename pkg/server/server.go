package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/fil-forge/go-libstoracha/ipnipublisher/store"
	"github.com/fil-forge/go-ucanto/principal"
	"github.com/fil-forge/go-ucanto/server"
	ucanretrieval "github.com/fil-forge/go-ucanto/server/retrieval"
	logging "github.com/ipfs/go-log/v2"
	"github.com/labstack/echo/v4"

	"github.com/fil-forge/piri/pkg/build"
	"github.com/fil-forge/piri/pkg/service/blobs"
	"github.com/fil-forge/piri/pkg/service/claims"
	"github.com/fil-forge/piri/pkg/service/publisher"
	"github.com/fil-forge/piri/pkg/service/retrieval"
	"github.com/fil-forge/piri/pkg/service/storage"
)

var log = logging.Logger("server")

type serverConfig struct {
	ucanSrvOpts          []server.Option
	ucanRetrievalSrvOpts []ucanretrieval.Option
}

type Option = func(c *serverConfig)

func WithUCANServerOptions(options ...server.Option) Option {
	return func(c *serverConfig) {
		c.ucanSrvOpts = options
	}
}

func WithUCANRetrievalServerOptions(options ...ucanretrieval.Option) Option {
	return func(c *serverConfig) {
		c.ucanRetrievalSrvOpts = options
	}
}

// ListenAndServe creates a new storage node HTTP server, and starts it up.
func ListenAndServe(addr string, storageSvc storage.Service, retrievalSvc retrieval.Service, options ...Option) error {
	srvMux, err := NewServer(storageSvc, retrievalSvc, options...)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:    addr,
		Handler: srvMux,
	}
	log.Infof("Listening on %s", addr)
	err = srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// NewServer creates a new storage node server.
func NewServer(storageSvc storage.Service, retrievalSvc retrieval.Service, options ...Option) (*echo.Echo, error) {
	cfg := serverConfig{}
	for _, opt := range options {
		opt(&cfg)
	}

	mux := echo.New()
	mux.GET("/", echo.WrapHandler(NewHandler(storageSvc.ID())))

	httpUcanSrv, err := storage.NewServer(storageSvc, cfg.ucanSrvOpts...)
	if err != nil {
		return nil, fmt.Errorf("creating UCAN server: %w", err)
	}
	httpUcanSrv.RegisterRoutes(mux)

	httpUcanRetrievalSrv, err := retrieval.NewServer(retrievalSvc, cfg.ucanRetrievalSrvOpts...)
	if err != nil {
		return nil, fmt.Errorf("creating UCAN retrieval server: %w", err)
	}
	httpUcanRetrievalSrv.RegisterRoutes(mux)

	httpClaimsSrv, err := claims.NewServer(storageSvc.Claims().Store())
	if err != nil {
		return nil, fmt.Errorf("creating claims server: %w", err)
	}
	httpClaimsSrv.RegisterRoutes(mux)

	httpBlobsSrv, err := blobs.NewServer(storageSvc.Blobs().Presigner(), storageSvc.Blobs().Allocations(), storageSvc.Blobs().Store())
	if err != nil {
		return nil, fmt.Errorf("creating blobs server: %w", err)
	}
	httpBlobsSrv.RegisterRoutes(mux)

	publisherStore := storageSvc.Claims().Publisher().Store()
	encodableStore, ok := publisherStore.(store.EncodeableStore)
	if !ok {
		return nil, errors.New("publisher store does not implement EncodableStore")
	}

	httpPublisherSrv, err := publisher.NewServer(encodableStore)
	if err != nil {
		return nil, fmt.Errorf("creating IPNI publisher server: %w", err)
	}
	httpPublisherSrv.RegisterRoutes(mux)

	return mux, nil
}

type ServerInfo struct {
	ID    string    `json:"id"`
	Build BuildInfo `json:"build"`
}

type BuildInfo struct {
	Version string `json:"version"`
	Repo    string `json:"repo"`
}

// NewHandler displays version info.
func NewHandler(id principal.Signer) http.Handler {
	info := ServerInfo{
		ID: id.DID().String(),
		Build: BuildInfo{
			Version: build.Version,
			Repo:    "https://github.com/fil-forge/piri",
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			w.Header().Set("Content-Type", "application/json")
			data, err := json.Marshal(&info)
			if err != nil {
				log.Errorf("failed JSON marshal server info: %w", err)
				http.Error(w, "failed JSON marshal server info", http.StatusInternalServerError)
				return
			}
			w.Write(data)
		} else {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte(fmt.Sprintf("🔥 piri %s\n", info.Build.Version)))
			w.Write([]byte("- https://github.com/fil-forge/piri\n"))
			w.Write([]byte(fmt.Sprintf("- %s", info.ID)))
		}
	})
}
