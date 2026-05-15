package signer_test

import (
	"encoding/hex"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/fil-forge/filecoin-services/go/eip712"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/client"
	"github.com/fil-forge/ucantone/did"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/service/proofs"
	signerclient "github.com/fil-forge/piri/pkg/service/signer"
)

// TestProofServiceSignerNoUpstream covers the misconfiguration where the
// signer wrapper is constructed without a connection (e.g. local-key mode
// was intended but the wrapper was wired anyway). Every Sign* call should
// surface the "no upstream" error rather than silently returning a nil
// signature.
func TestProofServiceSignerNoUpstream(t *testing.T) {
	// nil connection — wrapper falls into the unwired branch.
	var conn *client.HTTPClient
	proofService := proofs.NewCachingProofService()
	signingService := signerclient.NewProofServiceSigner(did.DID{}, conn, proofService)

	t.Run("pdp/sign/dataset/create", func(t *testing.T) {
		payee := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb")
		_, err := signingService.SignCreateDataSet(
			t.Context(),
			testutil.Alice,
			big.NewInt(1),
			payee,
			[]eip712.MetadataEntry{
				{Key: "name", Value: "test-dataset"},
				{Key: "version", Value: "1.0"},
			},
		)
		require.Error(t, err)
		require.True(t, errors.Is(err, errors.New("signer: no upstream piri-signing-service client configured")) ||
			err.Error() == "signer: no upstream piri-signing-service client configured",
			"unexpected error: %v", err)
	})

	t.Run("pdp/sign/dataset/delete", func(t *testing.T) {
		_, err := signingService.SignDeleteDataSet(t.Context(), testutil.Alice, big.NewInt(1))
		require.Error(t, err)
	})

	t.Run("pdp/sign/pieces/add", func(t *testing.T) {
		_, err := signingService.SignAddPieces(
			t.Context(),
			testutil.Alice,
			big.NewInt(1),
			big.NewInt(0),
			[][]byte{
				testutil.Must(hex.DecodeString("0001020304"))(t),
				testutil.Must(hex.DecodeString("0506070809"))(t),
			},
			[][]eip712.MetadataEntry{
				{{Key: "size", Value: "1024"}},
				{{Key: "size", Value: "2048"}},
			},
			nil,
			nil,
		)
		require.Error(t, err)
	})

	t.Run("pdp/sign/pieces/remove/schedule", func(t *testing.T) {
		_, err := signingService.SignSchedulePieceRemovals(
			t.Context(),
			testutil.Alice,
			big.NewInt(1),
			[]*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3)},
		)
		require.Error(t, err)
	})
}
