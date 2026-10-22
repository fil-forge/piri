package config

import (
	"bytes"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
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

// TestCommpConfig_Delay pins the three states of the delay: absent is the
// default, an explicit zero disables it, and a file written back by `setup
// install` (BurntSushi's encoder, as there) keeps the explicit zero and omits
// the absent key rather than turning one into the other.
func TestCommpConfig_Delay(t *testing.T) {
	t.Run("absent means the default", func(t *testing.T) {
		cfg := DefaultAggregationConfig()
		cfg.CommP.Delay = nil
		got, err := cfg.ToAppConfig()
		require.NoError(t, err)
		assert.Equal(t, commp.DefaultDelay, got.CommP.Delay)
	})

	t.Run("zero disables", func(t *testing.T) {
		cfg := DefaultAggregationConfig()
		cfg.CommP.Delay = ptr(time.Duration(0))
		got, err := cfg.ToAppConfig()
		require.NoError(t, err)
		assert.Zero(t, got.CommP.Delay)
	})

	t.Run("negative is rejected", func(t *testing.T) {
		cfg := DefaultAggregationConfig()
		cfg.CommP.Delay = ptr(-time.Second)
		_, err := cfg.ToAppConfig()
		require.ErrorContains(t, err, "must not be negative")
	})

	t.Run("above the ceiling is rejected", func(t *testing.T) {
		cfg := DefaultAggregationConfig()
		cfg.CommP.Delay = ptr(commp.MaxDelay + time.Second)
		_, err := cfg.ToAppConfig()
		require.ErrorContains(t, err, "exceeds")
	})

	t.Run("toml round trip keeps an explicit zero and omits an absent key", func(t *testing.T) {
		var zero CommpConfig
		_, err := toml.Decode(`delay = "0s"`, &zero)
		require.NoError(t, err)
		require.NotNil(t, zero.Delay)
		assert.Zero(t, *zero.Delay)
		var buf bytes.Buffer
		require.NoError(t, toml.NewEncoder(&buf).Encode(zero))
		assert.Contains(t, buf.String(), `delay = "0s"`)

		var absent CommpConfig
		_, err = toml.Decode(``, &absent)
		require.NoError(t, err)
		assert.Nil(t, absent.Delay)
		buf.Reset()
		require.NoError(t, toml.NewEncoder(&buf).Encode(absent))
		assert.NotContains(t, buf.String(), "delay")
	})
}
