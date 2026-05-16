//go:build legacy_ucan

package retrieval_test

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"

	blobcaps "github.com/fil-forge/go-libstoracha/capabilities/blob"
	"github.com/fil-forge/go-libstoracha/capabilities/space/content"
	ucancaps "github.com/fil-forge/go-libstoracha/capabilities/ucan"
	"github.com/fil-forge/go-libstoracha/testutil"
	"github.com/fil-forge/go-ucanto/client"
	retrievalclient "github.com/fil-forge/go-ucanto/client/retrieval"
	"github.com/fil-forge/go-ucanto/core/delegation"
	"github.com/fil-forge/go-ucanto/core/invocation"
	"github.com/fil-forge/go-ucanto/core/receipt"
	"github.com/fil-forge/go-ucanto/core/result"
	"github.com/fil-forge/go-ucanto/did"
	"github.com/fil-forge/go-ucanto/principal/absentee"
	"github.com/fil-forge/go-ucanto/transport"
	"github.com/fil-forge/go-ucanto/ucan"
	"github.com/fil-forge/go-ucanto/validator"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/fil-forge/piri/pkg/fx/app"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/internal/testutil/pdpfake"
	"github.com/fil-forge/piri/pkg/principalresolver"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

func TestFXSpaceContentRetrieve(t *testing.T) {
	var allocs allocationstore.AllocationStore
	var fakePieces *pdpfake.Pieces

	retrievalServiceID := testutil.Alice
	uploadServiceID := testutil.WebService
	uploadServiceURL := testutil.Must(url.Parse("https://up.test.storacha.network"))(t)

	appConfig := piritestutil.NewTestConfig(
		t,
		piritestutil.WithSigner(retrievalServiceID),
		piritestutil.WithUploadServiceConfig(uploadServiceID.DID(), uploadServiceURL),
	)
	testApp := fxtest.New(t,
		fx.NopLogger,
		app.CommonModules(appConfig),
		app.UCANModule,
		pdpfake.Module,
		// use the map resolver so no network calls are made that would fail anyway
		fx.Decorate(func() validator.PrincipalResolver {
			return testutil.Must(principalresolver.NewMapResolver(map[string]string{
				uploadServiceID.DID().String(): uploadServiceID.Unwrap().DID().String(),
			}))(t)
		}),
		fx.Populate(&allocs, &fakePieces),
	)

	testApp.RequireStart()
	defer testApp.RequireStop()
	piritestutil.WaitForHealthy(t, &appConfig.Server.PublicURL)

	t.Run("space/content/retrieve", func(t *testing.T) {
		space := testutil.RandomSigner(t)
		randBytes := testutil.RandomBytes(t, 256)
		blob := struct {
			bytes []byte
			cid   cid.Cid
		}{randBytes, cid.NewCidV1(cid.Raw, testutil.MultihashFromBytes(t, randBytes))}

		allocs.Put(t.Context(), allocation.Allocation{
			Blob: allocation.Blob{
				Digest: blob.cid.Hash(),
				Size:   uint64(len(blob.bytes)),
			},
			Space:   space.DID(),
			Expires: 0,
			Cause:   testutil.RandomCID(t),
		})
		fakePieces.Put(blob.cid.Hash(), blob.bytes)

		prf := delegation.FromDelegation(
			testutil.Must(
				delegation.Delegate(
					space,
					testutil.Bob,
					[]ucan.Capability[content.RetrieveCaveats]{
						ucan.NewCapability(
							content.RetrieveAbility,
							space.DID().String(),
							content.RetrieveCaveats{
								Blob:  content.BlobDigest{Digest: blob.cid.Hash()},
								Range: content.Range{Start: 0, End: uint64(len(blob.bytes) - 1)},
							},
						),
					},
				),
			)(t),
		)

		url := appConfig.Server.PublicURL.JoinPath("piece", blob.cid.String())
		conn, err := retrievalclient.NewConnection(testutil.Alice, url)
		require.NoError(t, err)

		inv, err := invocation.Invoke(
			testutil.Bob,
			testutil.Alice,
			content.Retrieve.New(
				space.DID().String(),
				content.RetrieveCaveats{
					Blob:  content.BlobDigest{Digest: blob.cid.Hash()},
					Range: content.Range{Start: 0, End: 1},
				},
			),
			delegation.WithProof(prf),
		)
		require.NoError(t, err)

		xres, hres, err := retrievalclient.Execute(t.Context(), inv, conn)
		require.NoError(t, err)

		assertContentRetrieveOK(t, inv.Link(), xres, hres, blob.bytes[:2], len(blob.bytes))
	})

	t.Run("space/content/retrieve with trusted upload service attestation", func(t *testing.T) {
		space := testutil.RandomSigner(t)
		randBytes := testutil.RandomBytes(t, 256)
		blob := struct {
			bytes []byte
			cid   cid.Cid
		}{randBytes, cid.NewCidV1(cid.Raw, testutil.MultihashFromBytes(t, randBytes))}

		allocs.Put(t.Context(), allocation.Allocation{
			Blob: allocation.Blob{
				Digest: blob.cid.Hash(),
				Size:   uint64(len(blob.bytes)),
			},
			Space:   space.DID(),
			Expires: 0,
			Cause:   testutil.RandomCID(t),
		})
		fakePieces.Put(blob.cid.Hash(), blob.bytes)

		account := absentee.From(testutil.Must(did.Parse("did:mailto:web.mail:bob"))(t))

		// delegation from space to bob's mailto account
		adminDlg, err := delegation.Delegate(
			space,
			account,
			[]ucan.Capability[ucan.NoCaveats]{
				ucan.NewCapability("*", "ucan:*", ucan.NoCaveats{}),
			},
		)
		require.NoError(t, err)

		// delegation from bob's mailto account to bob key
		accountDlg, err := delegation.Delegate(
			account,
			testutil.Bob,
			[]ucan.Capability[ucan.NoCaveats]{
				ucan.NewCapability(content.RetrieveAbility, space.DID().String(), ucan.NoCaveats{}),
			},
			delegation.WithProof(delegation.FromDelegation(adminDlg)),
		)
		require.NoError(t, err)

		// attestation from the upload service for bob's account delegation
		attestCaveats := ucancaps.AttestCaveats{
			Proof: accountDlg.Link(),
		}
		attestDlg, err := ucancaps.Attest.Delegate(
			uploadServiceID,
			testutil.Bob,
			uploadServiceID.DID().String(),
			attestCaveats,
		)
		require.NoError(t, err)

		url := appConfig.Server.PublicURL.JoinPath("piece", blob.cid.String())
		conn, err := retrievalclient.NewConnection(testutil.Alice, url)
		require.NoError(t, err)

		inv, err := invocation.Invoke(
			testutil.Bob,
			retrievalServiceID,
			content.Retrieve.New(
				space.DID().String(),
				content.RetrieveCaveats{
					Blob:  content.BlobDigest{Digest: blob.cid.Hash()},
					Range: content.Range{Start: 0, End: 1},
				},
			),
			delegation.WithProof(
				delegation.FromDelegation(accountDlg),
				delegation.FromDelegation(attestDlg),
			),
		)
		require.NoError(t, err)

		xres, hres, err := retrievalclient.Execute(t.Context(), inv, conn)
		require.NoError(t, err)

		assertContentRetrieveOK(t, inv.Link(), xres, hres, blob.bytes[:2], len(blob.bytes))
	})
}

func assertContentRetrieveOK(
	t *testing.T,
	task ucan.Link,
	xres client.ExecutionResponse,
	hres transport.HTTPResponse,
	expectBody []byte,
	expectTotalSize int,
) {
	expectStatus := http.StatusPartialContent
	expectHeaders := http.Header{
		http.CanonicalHeaderKey("Content-Length"): []string{fmt.Sprintf("%d", 2)},
		http.CanonicalHeaderKey("Content-Range"):  []string{fmt.Sprintf("bytes %d-%d/%d", 0, 1, expectTotalSize)},
	}

	require.Equal(t, expectStatus, hres.Status())
	for k, v := range expectHeaders {
		require.Equal(t, v, hres.Headers().Values(k))
	}
	require.Equal(t, expectBody, testutil.Must(io.ReadAll(hres.Body()))(t))

	rcptLink, ok := xres.Get(task)
	require.True(t, ok)

	rcpt, err := receipt.NewAnyReceiptReader().Read(rcptLink, xres.Blocks())
	require.NoError(t, err)

	_, x := result.Unwrap(rcpt.Out())
	require.Nil(t, x)
}

func TestFXBlobRetrieve(t *testing.T) {
	var fakePieces *pdpfake.Pieces

	appConfig := piritestutil.NewTestConfig(t, piritestutil.WithSigner(testutil.Alice))
	testApp := fxtest.New(t,
		fx.NopLogger,
		app.CommonModules(appConfig),
		app.UCANModule,
		pdpfake.Module,
		fx.Populate(&fakePieces),
	)

	testApp.RequireStart()
	defer testApp.RequireStop()
	piritestutil.WaitForHealthy(t, &appConfig.Server.PublicURL)

	t.Run("blob/retrieve", func(t *testing.T) {
		randBytes := testutil.RandomBytes(t, 256)
		blob := struct {
			bytes []byte
			cid   cid.Cid
		}{randBytes, cid.NewCidV1(cid.Raw, testutil.MultihashFromBytes(t, randBytes))}

		fakePieces.Put(blob.cid.Hash(), blob.bytes)

		prf := delegation.FromDelegation(
			testutil.Must(
				delegation.Delegate(
					testutil.Alice,
					testutil.Bob,
					[]ucan.Capability[blobcaps.RetrieveCaveats]{
						ucan.NewCapability(
							blobcaps.RetrieveAbility,
							testutil.Alice.DID().String(),
							blobcaps.RetrieveCaveats{
								Blob: blobcaps.Blob{Digest: blob.cid.Hash()},
							},
						),
					},
				),
			)(t),
		)

		url := appConfig.Server.PublicURL.JoinPath("piece", blob.cid.String())
		conn, err := retrievalclient.NewConnection(testutil.Alice, url)
		require.NoError(t, err)

		inv, err := invocation.Invoke(
			testutil.Bob,
			testutil.Alice,
			blobcaps.Retrieve.New(
				testutil.Alice.DID().String(),
				blobcaps.RetrieveCaveats{
					Blob: blobcaps.Blob{Digest: blob.cid.Hash()},
				},
			),
			delegation.WithProof(prf),
		)
		require.NoError(t, err)

		xres, hres, err := retrievalclient.Execute(t.Context(), inv, conn)
		require.NoError(t, err)

		expectStatus := http.StatusOK
		expectHeaders := http.Header{
			http.CanonicalHeaderKey("Content-Length"): []string{fmt.Sprintf("%d", len(blob.bytes))},
		}
		expectBody := blob.bytes

		require.Equal(t, expectStatus, hres.Status())
		for k, v := range expectHeaders {
			require.Equal(t, v, hres.Headers().Values(k))
		}
		require.Equal(t, expectBody, testutil.Must(io.ReadAll(hres.Body()))(t))

		rcptLink, ok := xres.Get(inv.Link())
		require.True(t, ok)

		rcpt, err := receipt.NewAnyReceiptReader().Read(rcptLink, xres.Blocks())
		require.NoError(t, err)

		_, x := result.Unwrap(rcpt.Out())
		require.Nil(t, x)
	})
}
