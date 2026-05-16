package proofs_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/fil-forge/libforge/capabilities/access"
	"github.com/fil-forge/ucantone/client"
	"github.com/fil-forge/ucantone/execution/bindexec"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/testutil"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/validator"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/service/proofs"
)

func TestCachingProofsService(t *testing.T) {
	webService := testutil.RandomSigner(t)
	alice := testutil.RandomSigner(t)
	bob := testutil.RandomSigner(t)

	// /access/grant is the bootstrap step in the access flow — any caller
	// must be able to self-issue it. Pass a permissive CanIssue so the
	// validator accepts the unauthenticated invocations from this test.
	srv := server.NewHTTP(
		webService,
		server.WithValidationOptions(validator.WithCanIssue(func(_ ucan.Capability, _ did.DID) bool {
			return true
		})),
	)
	srv.Handle(access.Grant, bindexec.NewHandler(
		func(req *bindexec.Request[*access.GrantArguments], res *bindexec.Response[*access.GrantOK]) error {
			args := req.Task().Arguments()
			require.NotEmpty(t, args.Attenuations)
			cmd := args.Attenuations[0].Command

			dlg, err := delegation.Delegate(
				webService,
				req.Invocation().Issuer(),
				webService.DID(),
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

	endpoint, err := url.Parse("http://test")
	require.NoError(t, err)
	httpClient, err := client.NewHTTP(endpoint, client.WithHTTPClient(&http.Client{Transport: srv}))
	require.NoError(t, err)

	proofsService := proofs.NewCachingProofService()

	command := ucan.Command("/test/test")
	dlg, err := proofsService.RequestAccess(t.Context(), alice, webService.DID(), command, nil, proofs.WithClient(httpClient))
	require.NoError(t, err)
	require.Equal(t, command, dlg.Command())
	require.Equal(t, webService.DID().String(), dlg.Subject().String())

	// delegation should be cached on a second call with the same args
	cacheDlg, err := proofsService.RequestAccess(t.Context(), alice, webService.DID(), command, nil, proofs.WithClient(httpClient))
	require.NoError(t, err)
	require.Equal(t, dlg.Nonce(), cacheDlg.Nonce())

	// same command but different issuer should fetch a fresh delegation
	otherDlg, err := proofsService.RequestAccess(t.Context(), bob, webService.DID(), command, nil, proofs.WithClient(httpClient))
	require.NoError(t, err)
	require.NotEqual(t, dlg.Link(), otherDlg.Link())

	// should get a fresh one if existing TTL is less than passed minimum
	freshDlg, err := proofsService.RequestAccess(
		t.Context(),
		alice,
		webService.DID(),
		command,
		nil,
		proofs.WithClient(httpClient),
		proofs.WithMinimumTTL(time.Hour),
	)
	require.NoError(t, err)
	require.NotEqual(t, dlg.Nonce(), freshDlg.Nonce())
}
