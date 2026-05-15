package storage_test

import (
	"io"
	"net/http"
	"testing"

	accesscaps "github.com/fil-forge/libforge/capabilities/access"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ipld/codec/dagcbor"
	ucanserver "github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/command"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/fil-forge/piri/pkg/fx/app"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
)

// TestFXAccessDelegateAndClaim exercises the round-trip:
//
//  1. An agent (Bob) builds a delegation that piri (the service) grants to
//     itself for `/test/test` against itself, and pushes the delegation to
//     piri via `/access/delegate` (with the delegation bytes attached as a
//     proof block).
//  2. The same agent invokes `/access/claim` against piri to retrieve the
//     stored delegation.
//
// The /access/delegate handler stores delegations into piri's delegation
// store keyed by root CID. /access/claim looks up delegations whose
// audience matches the invocation's issuer DID and returns their CIDs in
// the receipt, with the delegation bytes attached in the response container.
func TestFXAccessDelegateAndClaim(t *testing.T) {
	type srvParam struct {
		fx.In
		Server *ucanserver.HTTPServer `name:"storage_ucan_server"`
	}
	var srv srvParam

	piriID := testutil.Alice
	bob := testutil.Bob

	appConfig := piritestutil.NewTestConfig(t, piritestutil.WithSigner(piriID))
	testApp := fxtest.New(t,
		fx.NopLogger,
		app.CommonModules(appConfig),
		app.UCANModule,
		fx.Populate(&srv),
	)
	testApp.RequireStart()
	defer testApp.RequireStop()

	testCmd, err := command.Parse("/test/test")
	require.NoError(t, err)

	// Build a delegation: piri delegates `/test/test` to Bob.
	dlg, err := delegation.Delegate(
		piriID,
		bob.DID(),
		piriID.DID(),
		testCmd,
		delegation.WithNoExpiration(),
	)
	require.NoError(t, err)

	// Step 1: /access/delegate — Bob pushes the delegation to piri. Bob is
	// both the issuer and subject of the invocation (he's storing
	// delegations he authored). The audience is piri's identity, the
	// service that holds the storage.
	t.Run("delegate stores the delegation", func(t *testing.T) {
		delegateInv, err := accesscaps.Delegate.Invoke(
			bob,
			bob.DID(),
			&accesscaps.DelegateArguments{Delegations: []cid.Cid{dlg.Link()}},
			invocation.WithAudience(piriID.DID()),
		)
		require.NoError(t, err)

		ct := container.New(
			container.WithInvocations(delegateInv),
			container.WithDelegations(dlg),
		)
		rcpts := roundTrip(t, srv.Server, ct)
		require.Len(t, rcpts, 1)
		o, x := rcpts[0].Out().Unpack()
		require.Nil(t, x, "unexpected failure receipt")
		require.NotNil(t, o)
	})

	// Step 2: /access/claim — Bob retrieves delegations addressed to him.
	t.Run("claim returns the stored delegation", func(t *testing.T) {
		claimInv, err := accesscaps.Claim.Invoke(
			bob,
			bob.DID(),
			&accesscaps.ClaimArguments{},
			invocation.WithAudience(piriID.DID()),
		)
		require.NoError(t, err)

		ct := container.New(container.WithInvocations(claimInv))
		rcpts := roundTrip(t, srv.Server, ct)
		require.Len(t, rcpts, 1)

		// The response container should also carry the delegation bytes.
		// roundTrip returns only the receipts, so re-do the round-trip
		// directly here to inspect the container.
		respCt := serverRoundTrip(t, srv.Server, ct)
		require.Len(t, respCt.Receipts(), 1)
		require.GreaterOrEqual(t, len(respCt.Delegations()), 1, "expected at least one delegation in response container")

		var foundCID bool
		for _, d := range respCt.Delegations() {
			if d.Link() == dlg.Link() {
				foundCID = true
				break
			}
		}
		require.True(t, foundCID, "claimed delegation %s not present in response container", dlg.Link())
	})
}

// roundTrip POSTs a container to the storage UCAN server and returns the
// response container's receipts.
func roundTrip(t *testing.T, srv *ucanserver.HTTPServer, ct *container.Container) []ucan.Receipt {
	t.Helper()
	respCt := serverRoundTrip(t, srv, ct)
	return respCt.Receipts()
}

func serverRoundTrip(t *testing.T, srv *ucanserver.HTTPServer, ct *container.Container) *container.Container {
	t.Helper()
	r, w := io.Pipe()
	go func() {
		err := ct.MarshalCBOR(w)
		w.CloseWithError(err)
	}()

	req := http.Request{Header: http.Header{}, Body: r}
	req.Header.Set("Content-Type", dagcbor.ContentType)

	resp, err := srv.RoundTrip(&req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respCt := &container.Container{}
	err = respCt.UnmarshalCBOR(resp.Body)
	require.NoError(t, err)
	return respCt
}
