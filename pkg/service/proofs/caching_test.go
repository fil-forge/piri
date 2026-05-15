package proofs_test

import (
	"testing"
	"time"

	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/command"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/service/proofs"
)

func TestCachingProofsService(t *testing.T) {
	webService := testutil.WebService
	cmd, err := command.Parse("/test/test")
	require.NoError(t, err)

	t.Run("cache hit returns stored delegation", func(t *testing.T) {
		proofsService := proofs.NewCachingProofService()

		dlg, err := delegation.Delegate(
			webService,
			testutil.Alice.DID(),
			webService.DID(),
			cmd,
			delegation.WithNoExpiration(),
		)
		require.NoError(t, err)

		proofsService.Put(testutil.Alice.DID(), webService.DID(), cmd, dlg)

		got, err := proofsService.RequestAccess(t.Context(), testutil.Alice, webService.DID(), cmd, nil)
		require.NoError(t, err)
		require.Equal(t, dlg.Link(), got.Link())
	})

	t.Run("different issuer misses cache and triggers fetch", func(t *testing.T) {
		proofsService := proofs.NewCachingProofService()

		dlg, err := delegation.Delegate(
			webService,
			testutil.Alice.DID(),
			webService.DID(),
			cmd,
			delegation.WithNoExpiration(),
		)
		require.NoError(t, err)

		proofsService.Put(testutil.Alice.DID(), webService.DID(), cmd, dlg)

		// Different issuer (Bob): no cache entry. Without a configured
		// HTTP client RequestAccess can't run the /access/claim fetch and
		// returns ErrNoConnection.
		_, err = proofsService.RequestAccess(t.Context(), testutil.Bob, webService.DID(), cmd, nil)
		require.ErrorIs(t, err, proofs.ErrNoConnection)
	})

	t.Run("expired cache entry triggers fetch", func(t *testing.T) {
		proofsService := proofs.NewCachingProofService()

		dlg, err := delegation.Delegate(
			webService,
			testutil.Alice.DID(),
			webService.DID(),
			cmd,
			delegation.WithExpiration(ucan.Now()),
		)
		require.NoError(t, err)

		proofsService.Put(testutil.Alice.DID(), webService.DID(), cmd, dlg)

		// A long minimum TTL forces the cache entry to be treated as expired
		// even though it has a 1-second future expiration. Without a configured
		// HTTP client the fetch path returns ErrNoConnection.
		_, err = proofsService.RequestAccess(
			t.Context(),
			testutil.Alice,
			webService.DID(),
			cmd,
			nil,
			proofs.WithMinimumTTL(time.Hour),
		)
		require.ErrorIs(t, err, proofs.ErrNoConnection)
	})
}
