package service

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/fil-forge/piri/pkg/pdp/service/models"
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
			service := &PDPService{
				db:   setupProviderStatusTestDB(t),
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

// setupProviderStatusTestDB creates an in-memory SQLite database for provider status tests
func setupProviderStatusTestDB(t *testing.T) *gorm.DB {
	dbName := fmt.Sprintf("file:provider-status-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	require.NoError(t, err)

	sqlDb, err := db.DB()
	require.NoError(t, err)
	sqlDb.SetMaxOpenConns(1)

	err = db.AutoMigrate(&models.PDPProviderRegistration{})
	require.NoError(t, err)
	return db
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
