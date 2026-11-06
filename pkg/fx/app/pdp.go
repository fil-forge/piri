package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/fil-forge/piri/pkg/pdp/piece"
	"github.com/fil-forge/piri/pkg/pdp/pipeline"
	"github.com/fil-forge/piri/pkg/pdp/smartcontracts"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/gorilla/websocket"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/curiopdp"
	"github.com/fil-forge/piri/pkg/fx/pdp"
	"github.com/fil-forge/piri/pkg/pdp/service"
	"github.com/fil-forge/piri/pkg/store/blobstore"
	"github.com/fil-forge/piri/pkg/wallet"
)

var PDPModule = fx.Module("pdp",
	fx.Provide(
		ProvideEthClient, // provides concrete *ethclient.Client
		fx.Annotate(
			provideEthClientAsInterfaces,
			// provide as interface required by service(s)
			fx.As(new(service.EthClient)),
			fx.As(new(bind.ContractBackend)),
		),
		fx.Annotate(
			ProvideLotusClient,
			// provide as interface required by service(s)
			fx.As(new(service.ChainClient)),
		),
	),
	// Blobs received without their digest are staged until the settle task
	// moves them to their digest's key, so the blobstore every reader gets is
	// the staging store, reading through the staging index.
	fx.Provide(
		func(blobs *blobstore.Store, db *harmonydb.DB) *blobstore.StagingStore {
			return blobstore.NewStagingStore(blobs, service.NewStagingIndex(db))
		},
		func(staging *blobstore.StagingStore) blobstore.Blobstore { return staging },
	),
	smartcontracts.Module,
	pipeline.Module, // aggregation pipeline (commp/aggregate/add-roots) + removal sweep as harmonytasks
	curiopdp.Module, // Curio pdpv0 pipeline (harmonytask + prove/proving-period) on harmonydb
	pdp.Module,
	piece.Module,
	wallet.Module,
)

// provideEthClientAsInterfaces is a helper for fx.As to provide the concrete type as interfaces
func provideEthClientAsInterfaces(c *ethclient.Client) *ethclient.Client {
	return c
}

func ProvideEthClient(lc fx.Lifecycle, cfg app.PDPServiceConfig) (*ethclient.Client, error) {
	var opts []rpc.ClientOption
	if cfg.LotusAuthToken != "" {
		opts = append(opts, rpc.WithHeader("Authorization", "Bearer "+cfg.LotusAuthToken))
	}
	rpcClient, err := rpc.DialOptions(context.TODO(), cfg.LotusEndpoint.String(), opts...)
	if err != nil {
		return nil, fmt.Errorf("providing eth client: %w", err)
	}
	ethAPI := ethclient.NewClient(rpcClient)

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			ethAPI.Close()
			return nil
		},
	})
	return ethAPI, nil
}

func ProvideLotusClient(lc fx.Lifecycle, cfg app.PDPServiceConfig) (api.FullNode, error) {
	var header http.Header
	if cfg.LotusAuthToken != "" {
		header = http.Header{"Authorization": []string{"Bearer " + cfg.LotusAuthToken}}
	}
	lotusAPI, closer, err := client.NewFullNodeRPCV1(context.TODO(), cfg.LotusEndpoint.String(), header)
	if err != nil {
		return nil, fmt.Errorf("providing lotus client: %w", redactDialError(context.TODO(), cfg.LotusEndpoint, header, err))
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			closer()
			return nil
		},
	})
	return lotusAPI, nil
}

// redactDialError rewrites a failed chain RPC dial's error without the
// endpoint's query string or userinfo, which carry the API key for providers
// such as Chain.Love (?token=). go-jsonrpc prints the full address and drops
// the HTTP status of a refused handshake, so for a WebSocket endpoint the
// handshake is tried once more to read the status, which is appended the way
// go-ethereum reports it. The result does not wrap err: the wrapped error would
// still print the full address.
func redactDialError(ctx context.Context, endpoint *url.URL, header http.Header, err error) error {
	msg := strings.ReplaceAll(err.Error(), endpoint.String(), redactEndpoint(endpoint).String())
	if status := getHandshakeStatus(ctx, endpoint, header); status != "" {
		msg += " (HTTP status " + status + ")"
	}
	return errors.New(msg)
}

// redactEndpoint returns endpoint without its userinfo, query and fragment.
func redactEndpoint(endpoint *url.URL) *url.URL {
	redacted := *endpoint
	redacted.User = nil
	redacted.RawQuery = ""
	redacted.ForceQuery = false
	redacted.Fragment = ""
	redacted.RawFragment = ""
	return &redacted
}

// getHandshakeStatus dials a WebSocket endpoint and returns the HTTP status it
// refused the handshake with, or "" when it accepted the handshake, failed
// before answering, or is not a WebSocket endpoint.
func getHandshakeStatus(ctx context.Context, endpoint *url.URL, header http.Header) string {
	if endpoint.Scheme != "ws" && endpoint.Scheme != "wss" {
		return ""
	}
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, endpoint.String(), header)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil || resp == nil {
		return ""
	}
	_ = resp.Body.Close()
	return resp.Status
}
