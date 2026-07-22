package service

import (
	"context"
	"fmt"
	"math/big"

	"github.com/filecoin-project/curio/pdp/contract"

	"github.com/fil-forge/piri/pkg/pdp/types"
)

// RegisterProvider registers this node as a service provider by adopting Curio's
// contract.FSRegister: it reads the PDP signing key from the harmonydb eth_keys
// table, checks the wallet balance via the Lotus node, then signs and sends the
// ServiceProviderRegistry.registerProvider transaction directly. There is no
// local registration-tracking table — registration state lives entirely on-chain
// (queried via GetProviderStatus).
func (p *PDPService) RegisterProvider(ctx context.Context, params types.RegisterProviderParams) (types.RegisterProviderResults, error) {
	isRegistered, err := p.registryContract.IsRegisteredProvider(ctx, p.address)
	if err != nil {
		return types.RegisterProviderResults{}, fmt.Errorf("failed to check if service provider is registered: %w", err)
	}
	if isRegistered {
		return types.RegisterProviderResults{}, types.NewError(types.KindConflict, "Provider is already registered")
	}

	// Build the on-chain PDP offering. Only PaymentTokenAddress is consumed by the
	// service contract; the remaining fields are advertised on-chain as an unused
	// registry of provider metadata.
	offering := contract.PDPOfferingData{
		ServiceURL:               p.endpoint.String(),
		MinPieceSizeInBytes:      big.NewInt(1),
		MaxPieceSizeInBytes:      big.NewInt(1),
		IpniPiece:                false,
		IpniIpfs:                 false,
		StoragePricePerTibPerDay: big.NewInt(1),
		MinProvingPeriodInEpochs: big.NewInt(1),
		Location:                 "earth",
		PaymentTokenAddress:      p.address,
	}

	if err := contract.FSRegister(ctx, p.db, p.ethClient, params.Name, params.Description, offering, nil); err != nil {
		return types.RegisterProviderResults{}, fmt.Errorf("failed to register provider: %w", err)
	}

	// FSRegister fires the transaction without tracking it; registration is
	// confirmed by polling on-chain status (see GetProviderStatus).
	return types.RegisterProviderResults{
		Address: p.address,
	}, nil
}
