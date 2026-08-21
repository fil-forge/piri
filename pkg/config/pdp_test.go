package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/pdp/piecesize"
)

func TestPieceConfig_ToAppConfig(t *testing.T) {
	t.Run("zero means default", func(t *testing.T) {
		// An omitted [pdp.piece] section must not be an error.
		got, err := PieceConfig{}.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, piecesize.DefaultMaxPaddedSize, got.MaxPaddedSize)
	})

	t.Run("accepts a valid power of two", func(t *testing.T) {
		got, err := PieceConfig{MaxPaddedSize: 1 << 29}.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, uint64(1<<29), got.MaxPaddedSize)
	})

	t.Run("accepts the curio ceiling", func(t *testing.T) {
		got, err := PieceConfig{MaxPaddedSize: piecesize.CurioMaxPaddedSize}.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, piecesize.CurioMaxPaddedSize, got.MaxPaddedSize)
	})

	for _, tc := range []struct {
		name    string
		size    uint64
		wantErr string
	}{
		{"above the curio ceiling", 1 << 31, "exceeds the proving limit"},
		{"not a power of two", 3 << 27, "power of two"},
		{"below the minimum", 1 << 27, "below the minimum"},
	} {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			_, err := PieceConfig{MaxPaddedSize: tc.size}.ToAppConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "pdp.piece.max_padded_size")
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestPDPServiceConfig_ToAppConfig(t *testing.T) {
	validConfig := func() PDPServiceConfig {
		return PDPServiceConfig{
			OwnerAddress: "0x0000000000000000000000000000000000000001",
			ChainID:      "314159",
			PayerAddress: "0x0000000000000000000000000000000000000002",
			SigningService: SigningServiceConfig{
				DID: "did:web:signer.example.com",
				URL: "https://signer.example.com",
			},
			Contracts: ContractAddresses{
				Verifier:         "0x0000000000000000000000000000000000000003",
				ProviderRegistry: "0x0000000000000000000000000000000000000004",
				Service:          "0x0000000000000000000000000000000000000005",
				ServiceView:      "0x0000000000000000000000000000000000000006",
			},
			Aggregation: DefaultAggregationConfig(),
		}
	}

	t.Run("carries the lotus auth token through", func(t *testing.T) {
		cfg := validConfig()
		cfg.LotusAuthToken = "test-token"
		got, err := cfg.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, "test-token", got.LotusAuthToken)
	})

	t.Run("leaves the lotus auth token empty when unset", func(t *testing.T) {
		got, err := validConfig().ToAppConfig()
		require.NoError(t, err)
		assert.Empty(t, got.LotusAuthToken)
	})
}
