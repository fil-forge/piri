package service

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	libpiece "github.com/fil-forge/libforge/piece"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/fil-forge/piri/pkg/pdp/piece"
	"github.com/fil-forge/piri/pkg/pdp/service/models"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

// fakeResolver maps blob multihashes to piece multihashes.
type fakeResolver struct {
	pieces map[string]multihash.Multihash
}

func (f *fakeResolver) Resolve(_ context.Context, _ multihash.Multihash) (multihash.Multihash, bool, error) {
	panic("not implemented")
}

func (f *fakeResolver) ResolveToPiece(_ context.Context, blob multihash.Multihash) (multihash.Multihash, bool, error) {
	p, ok := f.pieces[blob.HexString()]
	return p, ok, nil
}

func (f *fakeResolver) ResolveToBlob(_ context.Context, _ multihash.Multihash) (multihash.Multihash, bool, error) {
	panic("not implemented")
}

func setupRemovalTest(t *testing.T) (*PDPService, blobstore.Blobstore, *fakeResolver) {
	t.Helper()
	dbName := fmt.Sprintf("file:removal-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&models.MessageWaitsEth{},
		&models.PDPProofSet{},
		&models.PDPProofsetRoot{},
		&models.ParkedPiece{},
		&models.ParkedPieceRef{},
		&models.PDPPieceRef{},
		&models.PDPPieceMHToCommp{},
		&models.PDPPendingRemoval{},
	))

	bs := blobstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
	resolver := &fakeResolver{pieces: map[string]multihash.Multihash{}}
	return &PDPService{db: db, blobstore: bs, pieceResolver: resolver}, bs, resolver
}

func commpCIDString(piece multihash.Multihash) string {
	return libpiece.MultihashToCommpCID(piece).String()
}

func mustMultihash(t *testing.T, data string) multihash.Multihash {
	h, err := multihash.Sum([]byte(data), multihash.SHA2_256, -1)
	require.NoError(t, err)
	return h
}

func TestRemovePiece_UnaggregatedDeletesImmediately(t *testing.T) {
	svc, bs, _ := setupRemovalTest(t)
	blob := mustMultihash(t, "blob-1")
	require.NoError(t, bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))

	require.NoError(t, svc.RemovePiece(t.Context(), blob))

	_, err := bs.Get(t.Context(), blob)
	require.ErrorIs(t, err, store.ErrNotFound, "unaggregated bytes deleted immediately")

	var count int64
	require.NoError(t, svc.db.Model(&models.PDPPendingRemoval{}).Count(&count).Error)
	require.Zero(t, count, "no pending-removal row for unaggregated blob")

	// Idempotent.
	require.NoError(t, svc.RemovePiece(t.Context(), blob))
}

func TestRemovePiece_AggregatedQueuesPendingRemoval(t *testing.T) {
	svc, bs, resolver := setupRemovalTest(t)
	blob := mustMultihash(t, "blob-1")
	piece := mustMultihash(t, "piece-1")
	resolver.pieces[blob.HexString()] = piece
	require.NoError(t, bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))

	require.NoError(t, svc.RemovePiece(t.Context(), blob))

	_, err := bs.Get(t.Context(), blob)
	require.NoError(t, err, "aggregated bytes retained until root retirement")

	var removals []models.PDPPendingRemoval
	require.NoError(t, svc.db.Find(&removals).Error)
	require.Len(t, removals, 1)
	require.Equal(t, models.PendingRemovalStatePending, removals[0].State)

	// Idempotent: re-removing does not duplicate the row.
	require.NoError(t, svc.RemovePiece(t.Context(), blob))
	require.NoError(t, svc.db.Find(&removals).Error)
	require.Len(t, removals, 1)
}

// seedRoot creates a proof set (id 1) with the given subroot piece CIDs under
// a single root (id 7), mirroring an aggregated batch.
func seedRoot(t *testing.T, db *gorm.DB, subroots []string) {
	t.Helper()
	require.NoError(t, db.Create(&models.MessageWaitsEth{SignedTxHash: "0xadd", TxStatus: "confirmed"}).Error)
	require.NoError(t, db.Create(&models.PDPProofSet{ID: 1, CreateMessageHash: "0xadd", Service: "test"}).Error)
	for i, sr := range subroots {
		require.NoError(t, db.Create(&models.PDPProofsetRoot{
			ProofsetID:     1,
			RootID:         7,
			SubrootOffset:  int64(i * 1024),
			Root:           "root-cid",
			AddMessageHash: "0xadd",
			Subroot:        sr,
			SubrootSize:    1024,
		}).Error)
	}
}

// TestRemovePiece_AcceptedBlobDefersWithRealResolver is the end-to-end
// regression for the resolver mhash key form: with the mh→commp row seeded
// exactly as CalculateCommP writes it (raw multihash bytes) and the piece
// live as a subroot, RemovePiece must queue a pending removal — never
// hard-delete bytes that are still being proven. A resolver that queries
// the mhash column by its string form misses the row and deletes the bytes.
func TestRemovePiece_AcceptedBlobDefersWithRealResolver(t *testing.T) {
	svc, bs, _ := setupRemovalTest(t)
	resolver, err := piece.NewStoreResolver(piece.StoreResolverParams{DB: svc.db})
	require.NoError(t, err)
	svc.pieceResolver = resolver

	blob := mustMultihash(t, "blob-real")
	pieceMH := mustMultihash(t, "piece-real")
	commp := commpCIDString(pieceMH)
	require.NoError(t, bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))

	// Seed as piece_commp.go does: raw multihash bytes in mhash.
	require.NoError(t, svc.db.Create(&models.PDPPieceMHToCommp{
		Mhash: blob, Size: 4, Commp: commp,
	}).Error)
	// The piece is a live subroot of an aggregate root.
	seedRoot(t, svc.db, []string{commp})

	require.NoError(t, svc.RemovePiece(t.Context(), blob))

	_, err = bs.Get(t.Context(), blob)
	require.NoError(t, err, "accepted blob's bytes must be retained until root retirement")
	var removals []models.PDPPendingRemoval
	require.NoError(t, svc.db.Find(&removals).Error)
	require.Len(t, removals, 1, "removal deferred via pending-removal queue")
	require.Equal(t, models.PendingRemovalStatePending, removals[0].State)
}

func TestProcessPendingRemovals_WaitsForWholeRootDeath(t *testing.T) {
	svc, _, resolver := setupRemovalTest(t)
	blobA, blobB := mustMultihash(t, "blob-a"), mustMultihash(t, "blob-b")
	pieceA, pieceB := mustMultihash(t, "piece-a"), mustMultihash(t, "piece-b")
	resolver.pieces[blobA.HexString()] = pieceA
	resolver.pieces[blobB.HexString()] = pieceB

	// Two subroots in the same root; only blobA is pending removal.
	commpA := commpCIDString(pieceA)
	commpB := commpCIDString(pieceB)
	seedRoot(t, svc.db, []string{commpA, commpB})
	require.NoError(t, svc.RemovePiece(t.Context(), blobA))

	removeRootCalls := 0
	removeRoot := func(context.Context, uint64, uint64) (common.Hash, error) {
		removeRootCalls++
		return common.HexToHash("0xdead"), nil
	}

	require.NoError(t, svc.processPendingRemovals(t.Context(), removeRoot))
	require.Zero(t, removeRootCalls, "root has a live subroot — not retired")

	var removal models.PDPPendingRemoval
	require.NoError(t, svc.db.First(&removal).Error)
	require.Equal(t, models.PendingRemovalStatePending, removal.State)

	// Releasing the last subroot triggers exactly one on-chain retirement.
	require.NoError(t, svc.RemovePiece(t.Context(), blobB))
	require.NoError(t, svc.processPendingRemovals(t.Context(), removeRoot))
	require.Equal(t, 1, removeRootCalls)

	var removals []models.PDPPendingRemoval
	require.NoError(t, svc.db.Find(&removals).Error)
	require.Len(t, removals, 2)
	for _, r := range removals {
		require.Equal(t, models.PendingRemovalStateScheduled, r.State)
		require.NotNil(t, r.ProofsetID)
		require.NotNil(t, r.RootID)
	}

	// Root rows still present (chain hasn't executed) — nothing to finalize,
	// and the root must not be re-scheduled.
	require.NoError(t, svc.processPendingRemovals(t.Context(), removeRoot))
	require.Equal(t, 1, removeRootCalls)
}

func TestProcessPendingRemovals_FinalizesAfterRootReaped(t *testing.T) {
	svc, bs, resolver := setupRemovalTest(t)
	blob := mustMultihash(t, "blob-a")
	piece := mustMultihash(t, "piece-a")
	resolver.pieces[blob.HexString()] = piece
	require.NoError(t, bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))

	commp := commpCIDString(piece)
	seedRoot(t, svc.db, []string{commp})
	// Bookkeeping rows finalization must clean up.
	require.NoError(t, svc.db.Create(&models.ParkedPiece{PieceCID: commp, PiecePaddedSize: 1024, PieceRawSize: 1000, LongTerm: true, Complete: true}).Error)
	require.NoError(t, svc.db.Create(&models.PDPPieceMHToCommp{Mhash: blob, Size: 4, Commp: commp}).Error)

	require.NoError(t, svc.RemovePiece(t.Context(), blob))
	removeRoot := func(context.Context, uint64, uint64) (common.Hash, error) {
		return common.HexToHash("0xdead"), nil
	}
	require.NoError(t, svc.processPendingRemovals(t.Context(), removeRoot))

	// Simulate the prove task's cleanupDeletedRoots reaping the root after
	// the chain executed the removal.
	require.NoError(t, svc.db.Where("proofset_id = ? AND root_id = ?", 1, 7).Delete(&models.PDPProofsetRoot{}).Error)

	require.NoError(t, svc.processPendingRemovals(t.Context(), removeRoot))

	_, err := bs.Get(t.Context(), blob)
	require.ErrorIs(t, err, store.ErrNotFound, "bytes deleted after root death")
	var count int64
	require.NoError(t, svc.db.Model(&models.PDPPendingRemoval{}).Count(&count).Error)
	require.Zero(t, count, "pending-removal row dropped")
	require.NoError(t, svc.db.Model(&models.ParkedPiece{}).Count(&count).Error)
	require.Zero(t, count, "parked piece row dropped")
	require.NoError(t, svc.db.Model(&models.PDPPieceMHToCommp{}).Count(&count).Error)
	require.Zero(t, count, "mhash→commp mapping dropped")
}

func TestProcessPendingRemovals_FailedTxReschedules(t *testing.T) {
	svc, _, resolver := setupRemovalTest(t)
	blob := mustMultihash(t, "blob-a")
	piece := mustMultihash(t, "piece-a")
	resolver.pieces[blob.HexString()] = piece

	commp := commpCIDString(piece)
	seedRoot(t, svc.db, []string{commp})
	require.NoError(t, svc.RemovePiece(t.Context(), blob))

	txHash := common.HexToHash("0xfailed")
	removeRoot := func(context.Context, uint64, uint64) (common.Hash, error) {
		return txHash, nil
	}
	require.NoError(t, svc.processPendingRemovals(t.Context(), removeRoot))

	// The removal tx confirmed but reverted.
	require.NoError(t, svc.db.Create(&models.MessageWaitsEth{
		SignedTxHash: txHash.String(),
		TxStatus:     "confirmed",
		TxSuccess:    models.Ptr(false),
	}).Error)

	require.NoError(t, svc.processPendingRemovals(t.Context(), removeRoot))

	var removal models.PDPPendingRemoval
	require.NoError(t, svc.db.First(&removal).Error)
	require.Equal(t, models.PendingRemovalStatePending, removal.State, "failed tx resets to pending for rescheduling")
	require.Nil(t, removal.RemoveMessageHash)
}
