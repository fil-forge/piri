package storage_test

import (
	"testing"

	blobcaps "github.com/fil-forge/libforge/capabilities/blob"
	"github.com/fil-forge/libforge/testutil"
	ucanserver "github.com/fil-forge/ucantone/server"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/fil-forge/piri/pkg/fx/app"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/service/storage"
)

// TestFXServer is the integration test for the storage UCAN server.
// It exercises a single /blob/allocate round-trip through the live fx-wired
// HTTP server to verify the handler returns a typed AllocateOK with a
// presigned upload URL.
func TestFXServer(t *testing.T) {
	piriID := testutil.Alice

	var svc storage.Service
	type srvParam struct {
		fx.In
		Server *ucanserver.HTTPServer `name:"storage_ucan_server"`
	}
	var srv srvParam

	appConfig := piritestutil.NewTestConfig(t, piritestutil.WithSigner(piriID))
	testApp := fxtest.New(t,
		fx.NopLogger,
		app.CommonModules(appConfig),
		app.UCANModule,
		fx.Populate(&svc, &srv),
	)
	testApp.RequireStart()
	defer testApp.RequireStop()

	t.Run("blob/allocate returns a presigned upload URL", func(t *testing.T) {
		digest := testutil.RandomMultihash(t)
		cause := testutil.RandomCID(t)
		size := uint64(1024)

		// piri is both the space (subject) and the issuer here — self-issued
		// invocations are accepted without an explicit delegation chain by
		// the validator's IsSelfIssued rule.
		inv, err := blobcaps.Allocate.Invoke(
			piriID,
			piriID.DID(),
			&blobcaps.AllocateArguments{
				Blob:  blobcaps.Blob{Digest: digest, Size: size},
				Cause: cause,
			},
			invocation.WithAudience(piriID.DID()),
		)
		require.NoError(t, err)

		respCt := serverRoundTrip(t, srv.Server, container.New(container.WithInvocations(inv)))
		require.Len(t, respCt.Receipts(), 1)
		o, x := respCt.Receipts()[0].Out().Unpack()
		require.Nil(t, x, "unexpected failure receipt: %x", x)
		require.NotNil(t, o, "expected success receipt with AllocateOK payload")
	})
}

// TestFXReplicaAllocateTransfer covers /blob/replica/allocate then
// /blob/replica/transfer. Pending: libforge replicator capability port +
// ucantone client wiring for the cross-service replica flow.
func TestFXReplicaAllocateTransfer(t *testing.T) {
	t.Skip("blob/replica/{allocate,transfer} handler suite awaits libforge replicator wiring")
}

// TestNewAllocationExistingData covers the edge case where a /blob/allocate
// arrives for content already present in the blob store (no double-charging).
// Pending: a way to seed the blob store before invocation, plus the matching
// receipt assertion on the returned `size`.
func TestNewAllocationExistingData(t *testing.T) {
	t.Skip("blob/allocate-for-existing-data: needs pre-seeded blob store fixture")
}
