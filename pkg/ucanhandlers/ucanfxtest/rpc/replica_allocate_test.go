package rpc_test

/*

import (
	"math/rand/v2"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fil-forge/go-libstoracha/capabilities/blob/replica"
	"github.com/fil-forge/go-ucanto/client"
	ucanserver "github.com/fil-forge/go-ucanto/server"
	"github.com/fil-forge/go-ucanto/validator"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	appconfig "github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/fx/app"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/internal/testutil/pdpfake"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

// TestFXReplicaAllocateTransfer validates the full replica allocation flow in the UCAN server,
// ensuring that invocations are correctly constructed and executed, and that the simulated endpoints
// interact as expected. A lightweight HTTP server (on port 8081) is used to simulate external endpoints:
//   - "/get": Represents the source node that returns the original blob data.
//   - "/put": Emulates the replica node that accepts and stores the blob.
//   - "/upload-service": Acts as the upload service by decoding a CAR payload and triggering a transfer receipt.
//
// This test covers three scenarios:
//  1. **NoExistingAllocationNoData:** No previous allocation or stored data exists, so the full blob is transferred.
//  2. **ExistingAllocationNoData:** An allocation record is present (indicating reserved space) but the blob data is not yet stored,
//     resulting in no additional data, but involving a transfer
//  3. **ExistingAllocationAndData:** Both an allocation record and the blob data are already present; although a transfer receipt is still produced,
//     no redundant data transfer should occur.
func (s *RPCSuite) TestFXReplicaAllocateTransfer() {
	testCases := []struct {
		name                  string
		hasExistingAllocation bool
		hasExistingData       bool
		expectedTransferSize  uint64
		simulateRetry         bool
		simulateFailure       bool
	}{
		{
			name:                  "NoExistingAllocationNoData",
			hasExistingAllocation: false,
			hasExistingData:       false,
		},
		{
			name:                  "ExistingAllocationNoData",
			hasExistingAllocation: true,
			hasExistingData:       false,
		},
		{
			name:                  "ExistingAllocationAndData",
			hasExistingAllocation: true,
			hasExistingData:       true,
		},
		{
			name:                  "TransferRetryAfterUploadServiceFailure",
			hasExistingAllocation: false,
			hasExistingData:       false,
			simulateRetry:         true, // Will fail upload service first, then succeed
		},
		{
			name:                  "TransferTotalFailure",
			hasExistingAllocation: false,
			hasExistingData:       false,
			simulateRetry:         false, // Will fail upload service first, then succeed
			simulateFailure:       true,
		},
	}

	for _, tc := range testCases {
		tc := tc // capture range variable
		s.T().Run(tc.name, func(t *testing.T) {
			// we expect each test to run in 60 seconds or less.
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)

			// Common setup: random DID, random data, etc.
			expectedSpace := testutil.RandomDID(t)
			expectedSize := uint64(rand.IntN(32) + 1)
			expectedData := testutil.RandomBytes(t, int(expectedSize))
			expectedDigest := testutil.Must(
				multihash.Sum(expectedData, multihash.SHA2_256, -1),
			)(t)
			replicas := uint(1)
			serverAddr := ":8081"
			sourcePath, sinkPath, uploadServicePath := "get", "put", "upload-service"

			// Spin up storage service, using injected values for testing.
			locationURL, uploadServiceURL, sinkURL := setupURLs(t, serverAddr, sourcePath, sinkPath, uploadServicePath)

			// Create test app configuration with custom presigner and upload service
			var (
				srv        ucanserver.ServerView[ucanserver.Service]
				fakePieces *pdpfake.Pieces
				allocs     allocationstore.AllocationStore
			)

			appConfig := piritestutil.NewTestConfig(t,
				piritestutil.WithSigner(testutil.Alice),
				piritestutil.WithUploadServiceConfig(testutil.WebService.DID(), uploadServiceURL),
			)

			testApp := fxtest.New(t,
				fx.NopLogger,
				app.CommonModules(appConfig),
				app.UCANModule,
				pdpfake.Module,
				// use the map resolver so no network calls are made that would fail anyway
				fx.Decorate(func() validator.PrincipalResolver {
					return testutil.Must(principalresolver.NewMapResolver(map[string]string{
						testutil.WebService.DID().String(): testutil.WebService.Unwrap().DID().String(),
					}))(t)
				}),
				// replace the default replicator config with one that causes failures to happen faster
				fx.Replace(appconfig.ReplicatorConfig{
					MaxRetries: 2,
					MaxWorkers: 1,
					MaxTimeout: time.Second,
				}),
				fx.Populate(&srv, &fakePieces, &allocs),
			)

			// Route the handler's WritePieceURL to the test sink endpoint.
			fakePieces.SetWriteURL(*sinkURL)

			testApp.RequireStart()

			fakeServer, transferOkChan, sourceGetCount, sinkPutCount := startTestHTTPServer(
				ctx, t, expectedDigest, expectedData, testutil.Alice, fakePieces,
				serverAddr, sourcePath, sinkPath, uploadServicePath,
				tc.simulateRetry, tc.simulateFailure,
			)

			t.Cleanup(func() {
				if err := fakeServer.Close(); err != nil {
					t.Logf("failed to close fake http server: %v", err)
				}
				testApp.RequireStop()
				cancel()
			})

			// Build UCAN server & connection
			conn := testutil.Must(client.NewConnection(testutil.Service, srv))(t)

			// Build UCAN delegation + location claim + replicate invocation
			// required ability's for blob replicate
			prf := buildDelegationProof(t)
			// location claim and blob replicate invocation, simulating an upload-service
			lcd, expectedLocationCaveats := buildLocationClaim(t, prf, expectedSpace, expectedDigest, locationURL, expectedSize)
			bri, expectedReplicaCaveats := buildReplicateInvocation(
				t, lcd, expectedDigest, expectedSize, replicas,
			)

			// Condition: If existing allocation, store an existing allocation
			// coverage when an allocation has been made but not transfered.
			if tc.hasExistingAllocation {
				require.NoError(t, allocs.Put(ctx, allocation.Allocation{
					Space: expectedSpace,
					Blob: allocation.Blob{
						Digest: expectedDigest,
						Size:   expectedSize,
					},
					Expires: uint64(time.Now().Add(time.Hour).UTC().Unix()),
					Cause:   bri.Link(),
				}))
			}

			// Condition: If existing data, store it in the blob store
			// covers when an allocation and replica already exist, meaning no transfer required.
			// though we still expect a transfer receipt.
			if tc.hasExistingData {
				fakePieces.Put(expectedDigest, expectedData)
			}

			// Build + execute the actual replica.Allocate invocation.
			// simulating an upload service sending the invocation to the storage node.
			rbi, expectedAllocateCaveats := buildAllocateInvocation(
				t, bri, lcd, expectedSpace, expectedDigest, expectedSize,
			)
			res, err := client.Execute(t.Context(), []invocation.Invocation{rbi}, conn)

			// Handle normal execution
			require.NoError(t, err)

			// The final assertion on the returned allocation size.
			// With an existing allocation or existing data, the new allocated
			// size is 0, otherwise it's expectedSize.
			var wantSize uint64
			if !tc.hasExistingAllocation && !tc.hasExistingData {
				// Normal case and first attempt of retry: new allocation gets the full size
				wantSize = expectedSize
			}

			// read the receipt for the blob allocate, asserting its size is expected value.
			alloc := mustReadAllocationReceipt(t, rbi, res)
			require.EqualValues(t, wantSize, alloc.Size)

			// Assert that the Site promise field exists and has the correct structure
			require.NotNil(t, alloc.Site)
			require.Equal(t, replica.AllocateSiteSelector, alloc.Site.UcanAwait.Selector)

			if tc.simulateRetry {
				// In retry scenario, first attempt fails at upload service
				// The transfer happens but upload service rejects it
				// So we won't get a message on the first attempt

				// Give the system time to process the failure and retry
				time.Sleep(500 * time.Millisecond)

				// Now manually trigger a retry by executing again
				t.Log("Triggering retry after upload service failure...")
				res2, err2 := client.Execute(t.Context(), []invocation.Invocation{rbi}, conn)
				require.NoError(t, err2, "retry should succeed")

				// Verify the retry allocation shows size 0 (blob already exists)
				alloc2 := mustReadAllocationReceipt(t, rbi, res2)
				require.EqualValues(t, 0, alloc2.Size, "Retry allocation should be 0 since blob exists")

				// This time we should get a transfer message since upload service will succeed
				ucanConcludeMsg := mustWaitForTransferMsg(t, ctx, transferOkChan)
				require.Len(t, ucanConcludeMsg.Invocations(), 1)
				// receipt is attached to the invocation, not a reciept in the message
				require.Len(t, ucanConcludeMsg.Receipts(), 0)

				// Full assertion on the retry transfer
				mustAssertTransferInvocation(
					t,
					ucanConcludeMsg,
					expectedDigest,
					0, // wantSize is 0 because blob already exists from first attempt
					expectedSpace,
					expectedLocationCaveats,
					expectedAllocateCaveats,
					expectedReplicaCaveats,
					tc.simulateFailure,
					fakePieces,
				)

				// Verify blob was only transferred once
				sourceCount := atomic.LoadInt32(sourceGetCount)
				sinkCount := atomic.LoadInt32(sinkPutCount)

				// The blob should only be transferred once (on first attempt)
				// Second attempt should NOT transfer again
				require.EqualValues(t, 1, sourceCount,
					"Source should only be hit once despite retry (idempotency test)")
				require.EqualValues(t, 1, sinkCount,
					"Sink should only be hit once despite retry (idempotency test)")

				t.Logf("Retry did NOT re-transfer blob as intended (source hits: %d, sink hits: %d)",
					sourceCount, sinkCount)
			} else {
				// Normal case - wait for transfer message
				ucanConcludeMsg := mustWaitForTransferMsg(t, ctx, transferOkChan)
				require.Len(t, ucanConcludeMsg.Invocations(), 1)
				// receipt is attached to the invocation, not a reciept in the message
				require.Len(t, ucanConcludeMsg.Receipts(), 0)

				// Full read + assertion on the transfer invocation and its ucan chain
				mustAssertTransferInvocation(
					t,
					ucanConcludeMsg,
					expectedDigest,
					wantSize,
					expectedSpace,
					expectedLocationCaveats,
					expectedAllocateCaveats,
					expectedReplicaCaveats,
					tc.simulateFailure,
					fakePieces,
				)
			}
		})
	}
}


*/
