package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/fil-forge/piri/pkg/pdp/piece"
	"github.com/fil-forge/piri/pkg/pdp/pipeline"
	"github.com/fil-forge/piri/pkg/pdp/smartcontracts"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"go.opentelemetry.io/otel"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/curiopdp"
	"github.com/fil-forge/piri/pkg/fx/pdp"
	"github.com/fil-forge/piri/pkg/pdp/chainrpc"
	"github.com/fil-forge/piri/pkg/pdp/service"
	"github.com/fil-forge/piri/pkg/store/blobstore"
	"github.com/fil-forge/piri/pkg/wallet"
)

var PDPModule = fx.Module("pdp",
	// Every consumer gets the chain clients through chainrpc's wrappers, as the
	// interfaces it needs, so each call is counted (piri_chain_rpc_calls). The
	// raw clients stay provided only for the wrappers to take.
	fx.Provide(
		ProvideEthClient,
		ProvideLotusClient,
		provideChainRPCMetrics,
		fx.Annotate(
			provideInstrumentedEthClient,
			fx.As(new(ethchain.EthClient)),
			fx.As(new(service.EthClient)),
			fx.As(new(bind.ContractBackend)),
		),
		fx.Annotate(
			provideInstrumentedLotusClient,
			fx.As(new(service.ChainClient)),
		),
	),
	fx.Invoke(startChainRPCProbe),
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

// provideChainRPCMetrics creates the chain RPC instruments on the global meter,
// which the serve command configures before wiring.
func provideChainRPCMetrics() (*chainrpc.Metrics, error) {
	return chainrpc.NewMetrics(otel.GetMeterProvider().Meter("github.com/fil-forge/piri/pkg/pdp/chainrpc"))
}

func provideInstrumentedEthClient(c *ethclient.Client, m *chainrpc.Metrics) *chainrpc.EthClient {
	return chainrpc.NewEthClient(c, m)
}

func provideInstrumentedLotusClient(n api.FullNode, m *chainrpc.Metrics) *chainrpc.LotusClient {
	return chainrpc.NewLotusClient(n, m)
}

// startChainRPCProbe probes the endpoint through both instrumented clients for
// the life of the app, so an idle Piri still reports whether it can reach it.
func startChainRPCProbe(lc fx.Lifecycle, m *chainrpc.Metrics, eth ethchain.EthClient, lotus service.ChainClient) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			chainrpc.StartProbe(ctx, m, eth, lotus)
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
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
		return nil, fmt.Errorf("providing lotus client: %w", err)
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			closer()
			return nil
		},
	})
	return lotusAPI, nil
}
