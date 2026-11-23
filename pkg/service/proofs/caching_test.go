package proofs_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/fil-forge/libforge/commands/access"
	"github.com/fil-forge/ucantone/binding"
	"github.com/fil-forge/ucantone/client"
	"github.com/fil-forge/ucantone/did/web"
	"github.com/fil-forge/ucantone/multikey"
	"github.com/fil-forge/ucantone/multikey/ed25519"
	"github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/testutil"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/command"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/service/proofs"
)

// TestCachingProofsService exercises CachingProofService.RequestAccess against
// an in-process /access/grant handler. It covers the four behaviors that
// callers rely on:
//
//  1. A fresh request returns a delegation that matches the requested command
//     and is rooted at the audience service.
//  2. A repeat request with the same (issuer, audience, command) tuple is
//     served from cache — proved by an unchanged nonce.
//  3. Changing the issuer bypasses the cache and triggers a server round-trip.
//  4. Requesting a minimum TTL longer than the cached delegation's remaining
//     lifetime forces a refresh, also producing a new nonce.
//
// The grant handler issues a short-lived delegation per call with a random
// nonce, so nonce equality across calls is a reliable cache-hit signal.
func TestCachingProofsService(t *testing.T) {
	webService := testutil.RandomIssuer(t)
	alice := testutil.RandomIssuer(t)
	bob := testutil.RandomIssuer(t)

	// /access/grant is the bootstrap step in the access flow; the proofs
	// service self-issues the invocation (subject == issuer), which the
	// validator accepts without any delegation chain.
	srv := server.NewHTTP(webService)
	srv.Handle(access.Grant.Command, binding.NewHandler(
		func(req *binding.Request[*access.GrantArguments], res *binding.Response[*access.GrantOK]) error {
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

	cmd := command.New("/test/test")
	dlg, err := proofsService.RequestAccess(t.Context(), alice, webService.DID(), cmd, nil, proofs.WithClient(httpClient))
	require.NoError(t, err)
	require.Equal(t, cmd, dlg.Command())
	require.Equal(t, webService.DID().String(), dlg.Subject().String())

	// delegation should be cached on a second call with the same args
	cacheDlg, err := proofsService.RequestAccess(t.Context(), alice, webService.DID(), cmd, nil, proofs.WithClient(httpClient))
	require.NoError(t, err)
	require.Equal(t, dlg.Nonce(), cacheDlg.Nonce())

	// same cmd but different issuer should fetch a fresh delegation
	otherDlg, err := proofsService.RequestAccess(t.Context(), bob, webService.DID(), cmd, nil, proofs.WithClient(httpClient))
	require.NoError(t, err)
	require.NotEqual(t, dlg.Link(), otherDlg.Link())

	// should get a fresh one if existing TTL is less than passed minimum
	freshDlg, err := proofsService.RequestAccess(
		t.Context(),
		alice,
		webService.DID(),
		cmd,
		nil,
		proofs.WithClient(httpClient),
		proofs.WithMinimumTTL(time.Hour),
	)
	require.NoError(t, err)
	require.NotEqual(t, dlg.Nonce(), freshDlg.Nonce())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestRequestAccessFindsTheServiceFromItsDID covers a request made with no
// service URL: the service is reached at the address its did:web names.
func TestRequestAccessFindsTheServiceFromItsDID(t *testing.T) {
	alice := testutil.RandomIssuer(t)
	key, err := ed25519.Generate()
	require.NoError(t, err)
	serviceDID, err := web.Parse("did:web:service.example%3A8443")
	require.NoError(t, err)
	service := multikey.NewIssuer(serviceDID, key)

	srv := server.NewHTTP(service)
	srv.Handle(access.Grant.Command, binding.NewHandler(
		func(req *binding.Request[*access.GrantArguments], res *binding.Response[*access.GrantOK]) error {
			dlg, err := delegation.Delegate(
				service,
				req.Invocation().Issuer(),
				service.DID(),
				req.Task().Arguments().Attenuations[0].Command,
				delegation.WithExpiration(ucan.Now()+30),
			)
			require.NoError(t, err)
			if err := res.SetMetadata(container.New(container.WithDelegations(dlg))); err != nil {
				return err
			}
			return res.SetSuccess(&access.GrantOK{Delegations: []cid.Cid{dlg.Link()}})
		},
	))

	var requested []string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.Scheme+"://"+r.URL.Host)
		return srv.RoundTrip(r)
	})}

	cmd := command.New("/test/test")
	dlg, err := proofs.NewCachingProofService().RequestAccess(t.Context(), alice, service.DID(), cmd, nil, proofs.WithHTTPClient(httpClient))
	require.NoError(t, err)
	require.Equal(t, cmd, dlg.Command())
	require.Equal(t, []string{"https://service.example:8443"}, requested)

	t.Run("an audience that is not a did:web needs a service URL", func(t *testing.T) {
		_, err := proofs.NewCachingProofService().RequestAccess(t.Context(), alice, testutil.RandomIssuer(t).DID(), cmd, nil)
		require.ErrorContains(t, err, "non-did:web audience")
	})
}
