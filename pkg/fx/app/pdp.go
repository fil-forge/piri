package app

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/fil-forge/piri/pkg/pdp/aggregation"
	"github.com/fil-forge/piri/pkg/pdp/piece"
	"github.com/fil-forge/piri/pkg/pdp/smartcontracts"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/curiopdp"
	"github.com/fil-forge/piri/pkg/fx/pdp"
	"github.com/fil-forge/piri/pkg/pdp/service"
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
	smartcontracts.Module,
	aggregation.Module,
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
	ethAPI, err := ethclient.Dial(cfg.LotusEndpoint.String())
	if err != nil {
		return nil, fmt.Errorf("providing eth client: %w", err)
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			ethAPI.Close()
			return nil
		},
	})
	return ethAPI, nil
}

func ProvideLotusClient(lc fx.Lifecycle, cfg app.PDPServiceConfig) (api.FullNode, error) {
	lotusAPI, closer, err := client.NewFullNodeRPCV1(context.TODO(), cfg.LotusEndpoint.String(), nil)
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
