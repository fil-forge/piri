package service

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/pdp/smartcontracts"
)

func TestRequireProviderRegistered(t *testing.T) {
	testCases := []struct {
		name         string
		isRegistered bool
		isApproved   bool
		wantErr      string
	}{
		{name: "provider registered and approved", isRegistered: true, isApproved: true},
		{name: "provider registered but not approved", isRegistered: true, isApproved: false},
		{name: "provider not registered", isRegistered: false, wantErr: "provider is not registered"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Registration status is read purely from the contracts; no DB
			// is involved on the Curio-based core.
			service := &PDPService{
				name: "storacha",
				registryContract: &mockRegistry{
					isRegistered: tc.isRegistered,
					providerInfo: &smartcontracts.ProviderInfo{ID: big.NewInt(1)},
				},
				serviceContract: &mockServiceContract{approved: tc.isApproved},
			}

			err := service.RequireProviderRegistered(context.Background())

			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}

// mockRegistry implements smartcontracts.Registry for testing
type mockRegistry struct {
	isRegistered bool
	providerInfo *smartcontracts.ProviderInfo
}

func (m *mockRegistry) IsRegisteredProvider(ctx context.Context, provider common.Address) (bool, error) {
	return m.isRegistered, nil
}

func (m *mockRegistry) GetProviderByAddress(ctx context.Context, provider common.Address) (*smartcontracts.ProviderInfo, error) {
	return m.providerInfo, nil
}

func (m *mockRegistry) Address() common.Address {
	return common.Address{}
}

// mockServiceContract implements smartcontracts.Service for testing; only
// IsProviderApproved is expected to be called.
type mockServiceContract struct {
	smartcontracts.Service
	approved bool
}

func (m *mockServiceContract) IsProviderApproved(ctx context.Context, providerId *big.Int) (bool, error) {
	return m.approved, nil
}
