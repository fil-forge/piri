package service

import (
	"context"
	"testing"
	"time"

	blobcaps "github.com/fil-forge/libforge/capabilities/blob"
	pdpcaps "github.com/fil-forge/libforge/capabilities/pdp"
	"github.com/fil-forge/libforge/digestutil"
	libforge_merkletree "github.com/fil-forge/libforge/merkletree"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/fil-forge/ucantone/ucan/receipt"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/sync"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

// TestGetAddPieceProofs exercises the function that bundles the
// (blob/accept, pdp/accept) receipts for the signing service.
//
// Phase 7d partial: the bundle currently carries only the receipts. Adding
// the corresponding invocations requires piri-side invocation-store writes
// in the /blob/accept handler. The test pins what is implemented today.
func TestGetAddPieceProofs(t *testing.T) {
	space := testutil.RandomSigner(t)
	size := 256
	blob := testutil.RandomMultihash(t)
	pieceCID := testutil.RandomCID(t)
	aggregateCID := testutil.RandomCID(t)

	blobAccInv, err := blobcaps.Accept.Invoke(
		testutil.WebService,
		space.DID(),
		&blobcaps.AcceptArguments{
			Blob: blobcaps.Blob{Digest: blob, Size: uint64(size)},
			Put:  promise.AwaitOK{Task: testutil.RandomCID(t)},
		},
		invocation.WithAudience(testutil.Alice.DID()),
	)
	require.NoError(t, err)

	pdpAccInv, err := pdpcaps.Accept.Invoke(
		testutil.Alice,
		testutil.Alice.DID(),
		&pdpcaps.AcceptArguments{Blob: blob},
	)
	require.NoError(t, err)

	blobAccRcpt, err := receipt.IssueOK(
		testutil.Alice,
		blobAccInv.Link(),
		&blobcaps.AcceptOK{Site: testutil.RandomCID(t)},
	)
	require.NoError(t, err)

	pdpAccRcpt, err := receipt.IssueOK(
		testutil.Alice,
		pdpAccInv.Link(),
		&pdpcaps.AcceptOK{
			Piece:          pieceCID,
			Aggregate:      aggregateCID,
			InclusionProof: libforge_merkletree.ProofData{},
		},
	)
	require.NoError(t, err)

	resolver := mockResolver{
		map[string]multihash.Multihash{
			digestutil.Format(pieceCID.Hash()): blob,
		},
	}

	accStore := acceptancestore.NewDatastoreStore(datastore.NewMapDatastore())
	err = accStore.Put(t.Context(), acceptance.Acceptance{
		Space: space.DID(),
		Blob: acceptance.Blob{
			Digest: blob,
			Size:   uint64(size),
		},
		PDPAccept: &acceptance.Promise{
			UcanAwait: acceptance.Await{
				Selector: ".out.ok",
				Link:     pdpAccInv.Link(),
			},
		},
		ExecutedAt: uint64(time.Now().Unix()),
		Cause:      blobAccInv.Link(),
	})
	require.NoError(t, err)

	rcptStore := receiptstore.NewDatastoreStore(sync.MutexWrap(datastore.NewMapDatastore()))
	require.NoError(t, rcptStore.Put(t.Context(), blobAccRcpt))
	require.NoError(t, rcptStore.Put(t.Context(), pdpAccRcpt))

	task, ct, err := getAddPieceProofs(t.Context(), &resolver, accStore, rcptStore, pieceCID)
	require.NoError(t, err)
	require.Equal(t, blobAccInv.Link(), task)

	// Both receipts should be present in the bundle.
	require.Len(t, ct.Receipts(), 2)
	rans := []string{}
	for _, r := range ct.Receipts() {
		rans = append(rans, r.Ran().String())
	}
	require.Contains(t, rans, blobAccInv.Link().String())
	require.Contains(t, rans, pdpAccInv.Link().String())

	// TODO(phase 7d follow-up): once /blob/accept persists its inbound
	// invocation, also assert the bundle contains both invocations:
	//   require.Contains(t, ct.Invocations(), blobAccInv)
	//   require.Contains(t, ct.Invocations(), pdpAccInv)
}

type mockResolver struct {
	data map[string]multihash.Multihash
}

func (r *mockResolver) ResolveToBlob(ctx context.Context, piece multihash.Multihash) (multihash.Multihash, bool, error) {
	key := digestutil.Format(piece)
	blobDigest, ok := r.data[key]
	return blobDigest, ok, nil
}
