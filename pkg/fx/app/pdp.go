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
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
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
	// Blobs received without their digest are held by their upload until
	// the commP task settles them, so every reader of the blobstore reads
	// through the upload index.
	fx.Decorate(func(bs blobstore.Blobstore, db *harmonydb.DB) blobstore.Blobstore {
		return blobstore.WithUploads(bs, service.NewUploadIndex(db))
	}),
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
