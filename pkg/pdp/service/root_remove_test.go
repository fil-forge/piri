package service

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/fil-forge/filecoin-services/go/bindings"
	"github.com/fil-forge/filecoin-services/go/eip712"
	"github.com/fil-forge/piri-signing-service/pkg/inprocess"
	signingservice "github.com/fil-forge/piri-signing-service/pkg/signer"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/pdp/smartcontracts"
)

// abiVerifier implements smartcontracts.Verifier for testing; only GetABI is
// expected to be called.
type abiVerifier struct {
	smartcontracts.Verifier
}

func (abiVerifier) GetABI() (*abi.ABI, error) {
	return bindings.PDPVerifierMetaData.GetAbi()
}

// removalCall is what schedulePieceDeletions calldata carries, with the
// extraData reduced to the address that signed it.
type removalCall struct {
	SetID    *big.Int
	PieceIDs []*big.Int
	Signer   common.Address
}

func TestSchedulePieceDeletionsCallIsSignedByThePayer(t *testing.T) {
	payerKey, err := crypto.GenerateKey() // throwaway key, test only
	require.NoError(t, err)
	chainID := big.NewInt(31337)
	serviceAddr := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	clientDataSetID := big.NewInt(7) // deliberately unlike the verifier set ID

	svc := &PDPService{
		signingService:   inprocess.New(signingservice.NewSigner(payerKey, chainID, serviceAddr)),
		edc:              eip712.NewExtraDataEncoder(),
		verifierContract: abiVerifier{},
		serviceContract: &mockServiceContract{
			dataSet: &smartcontracts.DataSetInfo{ClientDataSetId: clientDataSetID},
		},
	}

	data, err := svc.schedulePieceDeletionsCall(t.Context(), 3, 5)
	require.NoError(t, err)

	got := decodeRemovalCall(t, data, chainID, serviceAddr, clientDataSetID)
	require.Equal(t, removalCall{
		SetID:    big.NewInt(3),
		PieceIDs: []*big.Int{big.NewInt(5)},
		Signer:   crypto.PubkeyToAddress(payerKey.PublicKey),
	}, got)
}

// decodeRemovalCall unpacks schedulePieceDeletions calldata and recovers who
// signed its extraData, the way FWSS checks it: an EIP-712
// SchedulePieceRemovals over clientDataSetID and the call's piece IDs.
func decodeRemovalCall(t *testing.T, data []byte, chainID *big.Int, serviceAddr common.Address, clientDataSetID *big.Int) removalCall {
	t.Helper()
	verifierABI, err := bindings.PDPVerifierMetaData.GetAbi()
	require.NoError(t, err)
	method := verifierABI.Methods["schedulePieceDeletions"]
	require.Equal(t, method.ID, data[:4])
	args, err := method.Inputs.Unpack(data[4:])
	require.NoError(t, err)
	setID, pieceIDs, extraData := args[0].(*big.Int), args[1].([]*big.Int), args[2].([]byte)

	bytesType, err := abi.NewType("bytes", "", nil)
	require.NoError(t, err)
	decoded, err := abi.Arguments{{Type: bytesType}}.Unpack(extraData)
	require.NoError(t, err)
	sig := append([]byte{}, decoded[0].([]byte)...)
	require.Len(t, sig, 65)
	sig[64] -= 27 // contract form uses V of 27/28; recovery wants 0/1

	hash, err := eip712.GetMessageHash(eip712.GetDomain(chainID, serviceAddr), "SchedulePieceRemovals", map[string]interface{}{
		"clientDataSetId": clientDataSetID,
		"pieceIds":        pieceIDs,
	})
	require.NoError(t, err)
	pub, err := crypto.SigToPub(hash, sig)
	require.NoError(t, err)

	return removalCall{SetID: setID, PieceIDs: pieceIDs, Signer: crypto.PubkeyToAddress(*pub)}
}
