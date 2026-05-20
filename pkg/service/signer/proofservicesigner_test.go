package signer_test

import (
	"encoding/hex"
	"math/big"
	"math/rand/v2"
	"net/http"
	"net/url"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/fil-forge/filecoin-services/go/eip712"
	"github.com/fil-forge/libforge/commands/access"
	libforgesign "github.com/fil-forge/libforge/commands/pdp/sign"
	signerclient "github.com/fil-forge/piri-signing-service/pkg/client"
	"github.com/fil-forge/ucantone/client"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/principal"
	"github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/testutil"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/service/proofs"
	piriSigner "github.com/fil-forge/piri/pkg/service/signer"
)

func TestProofServiceSigner(t *testing.T) {
	signerServiceID := testutil.RandomSigner(t)
	srv := mockSigningServiceServer(t, signerServiceID)

	endpoint, err := url.Parse("http://test")
	require.NoError(t, err)
	httpClient, err := client.NewHTTP(endpoint, client.WithHTTPClient(&http.Client{Transport: srv}))
	require.NoError(t, err)

	sc := &signerclient.Client{ServiceDID: signerServiceID.DID(), HTTP: httpClient}
	proofService := proofs.NewCachingProofService()
	signingService := piriSigner.NewProofServiceSigner(sc, signerServiceID.DID(), httpClient, proofService)

	alice := testutil.RandomSigner(t)

	t.Run("pdp/sign/dataset/create", func(t *testing.T) {
		payee := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb")
		_, err := signingService.SignCreateDataSet(
			t.Context(),
			alice,
			randomBigInt(),
			payee,
			[]eip712.MetadataEntry{
				{Key: "name", Value: "test-dataset"},
				{Key: "version", Value: "1.0"},
			},
			nil,
		)
		require.NoError(t, err)
	})

	t.Run("pdp/sign/dataset/delete", func(t *testing.T) {
		_, err := signingService.SignDeleteDataSet(t.Context(), alice, randomBigInt(), nil)
		require.NoError(t, err)
	})

	t.Run("pdp/sign/pieces/add", func(t *testing.T) {
		_, err := signingService.SignAddPieces(
			t.Context(),
			alice,
			randomBigInt(),
			big.NewInt(0),
			[][]byte{
				mustHex(t, "0001020304"),
				mustHex(t, "0506070809"),
			},
			[][]eip712.MetadataEntry{
				{{Key: "size", Value: "1024"}},
				{{Key: "size", Value: "2048"}},
			},
			nil, // pieceProofs
			nil, // proofContainer
			nil, // proofs (the wrapper obtains its own access grant)
		)
		require.NoError(t, err)
	})

	t.Run("pdp/sign/pieces/remove/schedule", func(t *testing.T) {
		_, err := signingService.SignSchedulePieceRemovals(
			t.Context(),
			alice,
			randomBigInt(),
			[]*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3)},
			nil,
		)
		require.NoError(t, err)
	})
}

func mockSigningServiceServer(t *testing.T, id principal.Signer) *server.HTTPServer {
	mock := mockLibforgeSignature()
	srv := server.NewHTTP(
		id,
	)
	/*
		server.WithValidationOptions(validator.WithCanIssue(func(_ ucan.Capability, _ did.DID) bool {
			return true
		})),

	*/

	srv.Handle(access.Grant.Command, bindexec.NewHandler(
		func(req *bindexec.Request[*access.GrantArguments], res *bindexec.Response[*access.GrantOK]) error {
			args := req.Task().Arguments()
			require.NotEmpty(t, args.Attenuations)
			cmd := args.Attenuations[0].Command

			dlg, err := delegation.Delegate(
				id,
				req.Invocation().Issuer(),
				id.DID(),
				cmd,
				delegation.WithExpiration(ucan.Now()+30),
				delegation.WithNonce(testutil.RandomBytes(t, 16)),
			)
			require.NoError(t, err)

			if err := res.SetMetadata(container.New(container.WithDelegations(dlg))); err != nil {
				return err
			}
			return res.SetSuccess(&access.GrantOK{Delegations: []cid.Cid{dlg.Link()}})
		},
	))

	srv.Handle(libforgesign.DataSetCreate.Command, bindexec.NewHandler(
		func(_ *bindexec.Request[*libforgesign.DataSetCreateArguments], res *bindexec.Response[*libforgesign.DataSetCreateOK]) error {
			return res.SetSuccess(&mock)
		},
	))
	srv.Handle(libforgesign.DataSetDelete.Command, bindexec.NewHandler(
		func(_ *bindexec.Request[*libforgesign.DataSetDeleteArguments], res *bindexec.Response[*libforgesign.DataSetDeleteOK]) error {
			return res.SetSuccess(&mock)
		},
	))
	srv.Handle(libforgesign.PiecesAdd.Command, bindexec.NewHandler(
		func(_ *bindexec.Request[*libforgesign.PiecesAddArguments], res *bindexec.Response[*libforgesign.PiecesAddOK]) error {
			return res.SetSuccess(&mock)
		},
	))
	srv.Handle(libforgesign.PiecesRemoveSchedule.Command, bindexec.NewHandler(
		func(_ *bindexec.Request[*libforgesign.PiecesRemoveScheduleArguments], res *bindexec.Response[*libforgesign.PiecesRemoveScheduleOK]) error {
			return res.SetSuccess(&mock)
		},
	))

	return srv
}

func mockLibforgeSignature() libforgesign.AuthSignature {
	return libforgesign.AuthSignature{
		Signature:  []byte{0x01, 0x02, 0x03},
		V:          27,
		R:          common.BigToHash(big.NewInt(12345)).Bytes(),
		S:          common.BigToHash(big.NewInt(67890)).Bytes(),
		SignedData: []byte{0xaa, 0xbb},
		Signer:     common.HexToAddress("0x1234567890123456789012345678901234567890").Bytes(),
	}
}

func randomBigInt() *big.Int {
	return new(big.Int).SetInt64(int64(rand.Uint32()) + 1)
}

func mustHex(t *testing.T, s string) []byte {
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}
