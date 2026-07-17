package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	blobcmd "github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"

	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

// These tests pin the four holes of the accepted-blob removal race (see
// ProcessPendingRemovals): pre-commp cancellation, mid-aggregation safety,
// revived-claim cancellation, and whole-root retirement. They run against a
// real harmonydb (Postgres testcontainer) with the curated PDP schema, the
// same substrate the pipeline tasks use.

type removalWorld struct {
	svc     *PDPService
	db      *harmonydb.DB
	bs      blobstore.Blobstore
	accepts *acceptancestore.Store
	allocs  *allocationstore.Store
}

func setupRemovalTest(t *testing.T) *removalWorld {
	t.Helper()
	db := piritestutil.NewHarmonyDB(t)
	w := &removalWorld{
		db:      db,
		bs:      blobstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore())),
		accepts: acceptancestore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore())),
		allocs:  allocationstore.NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore())),
	}
	w.svc = &PDPService{
		db:              db,
		blobstore:       w.bs,
		acceptanceStore: w.accepts,
		allocationStore: w.allocs,
	}
	return w
}

func noopRemoveRoot(context.Context, uint64, uint64) (common.Hash, error) {
	return common.HexToHash("0xdead"), nil
}

// stampingRemoveRoot mimics the real RemoveRoot's side effect: stamping
// rm_message_hash on every sub-piece row of the retired root.
func (w *removalWorld) stampingRemoveRoot(t *testing.T, calls *int) func(context.Context, uint64, uint64) (common.Hash, error) {
	return func(ctx context.Context, dataSet, pieceID uint64) (common.Hash, error) {
		*calls++
		_, err := w.db.Exec(ctx, `
			UPDATE pdp_data_set_pieces SET rm_message_hash = $1
			WHERE data_set = $2 AND piece_id = $3
		`, "0xdead", int64(dataSet), int64(pieceID))
		require.NoError(t, err)
		return common.HexToHash("0xdead"), nil
	}
}

func mustMultihash(t *testing.T, data string) multihash.Multihash {
	h, err := multihash.Sum([]byte(data), multihash.SHA2_256, -1)
	require.NoError(t, err)
	return h
}

// testPiece derives a valid (v2, v1) commP CID pair from a seed, the same
// way CalculateCommP does (DataCommitment → v1 → v2 with raw size).
func testPiece(t *testing.T, seed string) (commpV2, commpV1 string) {
	t.Helper()
	digest := sha256.Sum256([]byte(seed))
	digest[31] &= 0b00111111 // valid fr32 commitment: top bits clear
	v1, err := commcid.DataCommitmentV1ToCID(digest[:])
	require.NoError(t, err)
	v2, err := commcid.PieceCidV2FromV1(v1, 1024)
	require.NoError(t, err)
	return v2.String(), v1.String()
}

// --- seeding helpers (raw SQL, seeded exactly as production writes) ---

func (w *removalWorld) seedPipeline(t *testing.T, blob multihash.Multihash, commp string, aggregateRoot string) {
	t.Helper()
	var commpArg, rootArg any
	if commp != "" {
		commpArg = commp
	}
	if aggregateRoot != "" {
		rootArg = aggregateRoot
	}
	_, err := w.db.Exec(t.Context(), `
		INSERT INTO pdp_blob_pipeline (blob, commp_task_id, commp, aggregate_root)
		VALUES ($1, 42, $2, $3)
	`, []byte(blob), commpArg, rootArg)
	require.NoError(t, err)
}

func (w *removalWorld) seedMapping(t *testing.T, blob multihash.Multihash, commpV2, commpV1 string) {
	t.Helper()
	_, err := w.db.Exec(t.Context(), `
		INSERT INTO pdp_piece_mh_to_commp (mhash, size, commp, commp_v1)
		VALUES ($1, 4, $2, $3)
	`, []byte(blob), commpV2, commpV1)
	require.NoError(t, err)
}

// seedParkedChain creates the pdp_services → parked_pieces →
// parked_piece_refs → pdp_piecerefs chain as ParkPiece leaves it, returning
// the pdp_piecerefs id (needed by data-set piece rows).
func (w *removalWorld) seedParkedChain(t *testing.T, commpV2 string) int64 {
	t.Helper()
	ctx := t.Context()
	_, err := w.db.Exec(ctx, `
		INSERT INTO pdp_services (id, pubkey, service_label)
		VALUES (1, $1, 'storacha') ON CONFLICT DO NOTHING
	`, []byte{1})
	require.NoError(t, err)

	var pieceID int64
	require.NoError(t, w.db.QueryRow(ctx, `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, complete)
		VALUES ($1, 1024, 1000, TRUE, TRUE) RETURNING id
	`, commpV2).Scan(&pieceID))

	var refID int64
	require.NoError(t, w.db.QueryRow(ctx, `
		INSERT INTO parked_piece_refs (piece_id, data_url, long_term, data_headers)
		VALUES ($1, 'pdpstore://x', TRUE, '{}'::jsonb) RETURNING ref_id
	`, pieceID).Scan(&refID))

	var pieceRefID int64
	require.NoError(t, w.db.QueryRow(ctx, `
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref)
		VALUES ('storacha', $1, $2) RETURNING id
	`, commpV2, refID).Scan(&pieceRefID))
	return pieceRefID
}

// seedAddMessage satisfies the FKs on the data-set piece tables:
// add_message_hash → message_waits_eth and data_set → pdp_data_sets(id=1).
// Idempotent.
func (w *removalWorld) seedAddMessage(t *testing.T) {
	t.Helper()
	_, err := w.db.Exec(t.Context(), `
		INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
		VALUES ('0xadd', 'confirmed') ON CONFLICT DO NOTHING
	`)
	require.NoError(t, err)
	_, err = w.db.Exec(t.Context(), `
		INSERT INTO pdp_data_sets (id, create_message_hash, service)
		VALUES (1, '0xadd', 'storacha') ON CONFLICT DO NOTHING
	`)
	require.NoError(t, err)
}

// seedPieceRow stages a live sub-piece row under (data_set=1, piece_id),
// mirroring a confirmed addPieces.
func (w *removalWorld) seedPieceRow(t *testing.T, pieceID int64, rootV1, subV1 string, offset, pieceRef int64) {
	t.Helper()
	w.seedAddMessage(t)
	_, err := w.db.Exec(t.Context(), `
		INSERT INTO pdp_data_set_pieces
			(data_set, piece, add_message_hash, add_message_index, piece_id,
			 sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref)
		VALUES (1, $1, '0xadd', 0, $2, $3, $4, 1024, $5)
	`, rootV1, pieceID, subV1, offset, pieceRef)
	require.NoError(t, err)
}

// count returns the row count of a table. harmonyquery only accepts literal
// query strings, hence the switch.
func (w *removalWorld) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	var err error
	ctx := t.Context()
	switch table {
	case "pdp_pending_piece_removals":
		err = w.db.QueryRow(ctx, `SELECT count(*) FROM pdp_pending_piece_removals`).Scan(&n)
	case "pdp_blob_pipeline":
		err = w.db.QueryRow(ctx, `SELECT count(*) FROM pdp_blob_pipeline`).Scan(&n)
	case "pdp_piecerefs":
		err = w.db.QueryRow(ctx, `SELECT count(*) FROM pdp_piecerefs`).Scan(&n)
	case "parked_pieces":
		err = w.db.QueryRow(ctx, `SELECT count(*) FROM parked_pieces`).Scan(&n)
	case "pdp_data_set_pieces":
		err = w.db.QueryRow(ctx, `SELECT count(*) FROM pdp_data_set_pieces`).Scan(&n)
	case "pdp_piece_mh_to_commp":
		err = w.db.QueryRow(ctx, `SELECT count(*) FROM pdp_piece_mh_to_commp`).Scan(&n)
	default:
		t.Fatalf("count: unknown table %s", table)
	}
	require.NoError(t, err)
	return n
}

// --- the regressions ---

// TestRemovePiece_NeverDeletesInline is the core contract of the redesign:
// RemovePiece only queues; the bytes survive until the sweep proves the
// pipeline is done with the blob. Idempotent.
func TestRemovePiece_NeverDeletesInline(t *testing.T) {
	w := setupRemovalTest(t)
	blob := mustMultihash(t, "blob-1")
	require.NoError(t, w.bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))

	require.NoError(t, w.svc.RemovePiece(t.Context(), blob))

	_, err := w.bs.Get(t.Context(), blob)
	require.NoError(t, err, "RemovePiece must never delete inline")
	require.Equal(t, 1, w.count(t, "pdp_pending_piece_removals"))

	require.NoError(t, w.svc.RemovePiece(t.Context(), blob))
	require.Equal(t, 1, w.count(t, "pdp_pending_piece_removals"))
}

// TestSweep_NeverAggregatedFinalizes: no claims, no pipeline row, no mapping
// — the sweep deletes the bytes on its next pass (the parked-blob reject
// case, now asynchronous).
func TestSweep_NeverAggregatedFinalizes(t *testing.T) {
	w := setupRemovalTest(t)
	blob := mustMultihash(t, "blob-1")
	require.NoError(t, w.bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))
	require.NoError(t, w.svc.RemovePiece(t.Context(), blob))

	require.NoError(t, w.svc.processPendingRemovals(t.Context(), noopRemoveRoot))

	_, err := w.bs.Get(t.Context(), blob)
	require.ErrorIs(t, err, store.ErrNotFound, "never-aggregated bytes deleted by the sweep")
	require.Zero(t, w.count(t, "pdp_pending_piece_removals"))

	// Idempotent after finalization.
	require.NoError(t, w.svc.processPendingRemovals(t.Context(), noopRemoveRoot))
}

// TestSweep_PreCommpPipelineEntryCancelled is the accepted-then-removed
// pre-commp hole: the blob is in the pipeline but its commp hasn't been
// computed. The sweep cancels the row (the in-flight commp task no-ops on
// the missing row) and only then releases the bytes.
func TestSweep_PreCommpPipelineEntryCancelled(t *testing.T) {
	w := setupRemovalTest(t)
	blob := mustMultihash(t, "blob-precommp")
	require.NoError(t, w.bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))
	w.seedPipeline(t, blob, "", "")
	require.NoError(t, w.svc.RemovePiece(t.Context(), blob))

	require.NoError(t, w.svc.processPendingRemovals(t.Context(), noopRemoveRoot))

	require.Zero(t, w.count(t, "pdp_blob_pipeline"),
		"pre-aggregation pipeline row cancelled")
	_, err := w.bs.Get(t.Context(), blob)
	require.ErrorIs(t, err, store.ErrNotFound, "bytes released after cancellation")
}

// TestSweep_BufferedPieceCancelled: a commP'd piece waiting in the
// aggregation buffer (aggregate_root NULL) is cancellable — the fold locks
// its candidate rows, so a row the sweep deletes can never be folded. The
// orphaned parked chain is cleaned up by finalization.
func TestSweep_BufferedPieceCancelled(t *testing.T) {
	w := setupRemovalTest(t)
	blob := mustMultihash(t, "blob-buffered")
	commpV2, commpV1 := testPiece(t, "piece-buffered")
	require.NoError(t, w.bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))
	w.seedPipeline(t, blob, commpV2, "")
	w.seedMapping(t, blob, commpV2, commpV1)
	w.seedParkedChain(t, commpV2)
	require.NoError(t, w.svc.RemovePiece(t.Context(), blob))

	require.NoError(t, w.svc.processPendingRemovals(t.Context(), noopRemoveRoot))

	require.Zero(t, w.count(t, "pdp_blob_pipeline"), "buffered row cancelled")
	require.Zero(t, w.count(t, "pdp_pending_piece_removals"))
	require.Zero(t, w.count(t, "pdp_piecerefs"), "parked chain released")
	require.Zero(t, w.count(t, "parked_pieces"))
	_, err := w.bs.Get(t.Context(), blob)
	require.ErrorIs(t, err, store.ErrNotFound, "buffered bytes released")
}

// TestSweep_FoldedAggregateWaits is the worst-ordering hole: the blob's
// piece has been folded into an aggregate (aggregate_root set) that hasn't
// been staged yet. The sweep must NOT delete the bytes — the aggregate
// would prove deleted data on-chain — and must wait for the piece to
// surface as a sub-piece and ride the root-retirement path.
func TestSweep_FoldedAggregateWaits(t *testing.T) {
	w := setupRemovalTest(t)
	blob := mustMultihash(t, "blob-folded")
	commpV2, commpV1 := testPiece(t, "piece-folded")
	rootV2, _ := testPiece(t, "root-folded")
	require.NoError(t, w.bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))
	w.seedPipeline(t, blob, commpV2, rootV2)
	w.seedMapping(t, blob, commpV2, commpV1)
	w.seedParkedChain(t, commpV2)
	require.NoError(t, w.svc.RemovePiece(t.Context(), blob))

	require.NoError(t, w.svc.processPendingRemovals(t.Context(), noopRemoveRoot))

	_, err := w.bs.Get(t.Context(), blob)
	require.NoError(t, err, "bytes folded into an unstaged aggregate must never be deleted")
	require.Equal(t, 1, w.count(t, "pdp_blob_pipeline"),
		"folded row is not cancellable")
	require.Equal(t, 1, w.count(t, "pdp_pending_piece_removals"),
		"removal waits")
}

// TestSweep_StagedUnconfirmedWaits: the piece's addPieces tx is staged but
// unconfirmed (pdp_data_set_piece_adds). The sweep waits for the chain
// watcher to settle it before classifying.
func TestSweep_StagedUnconfirmedWaits(t *testing.T) {
	w := setupRemovalTest(t)
	blob := mustMultihash(t, "blob-staged")
	commpV2, commpV1 := testPiece(t, "piece-staged")
	rootV2, rootV1 := testPiece(t, "root-staged")
	_ = rootV2
	require.NoError(t, w.bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))
	w.seedMapping(t, blob, commpV2, commpV1)
	pieceRef := w.seedParkedChain(t, commpV2)
	w.seedAddMessage(t)
	_, err := w.db.Exec(t.Context(), `
		INSERT INTO pdp_data_set_piece_adds
			(data_set, piece, add_message_hash, add_message_index,
			 sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref, pieces_added)
		VALUES (1, $1, '0xadd', 0, $2, 0, 1024, $3, FALSE)
	`, rootV1, commpV1, pieceRef)
	require.NoError(t, err)
	require.NoError(t, w.svc.RemovePiece(t.Context(), blob))

	require.NoError(t, w.svc.processPendingRemovals(t.Context(), noopRemoveRoot))

	_, err = w.bs.Get(t.Context(), blob)
	require.NoError(t, err, "bytes retained while the add is unconfirmed")
	require.Equal(t, 1, w.count(t, "pdp_pending_piece_removals"))
}

// TestSweep_RevivedClaimCancelsRemoval: a racing accept (or a re-upload of
// the same content) between RemovePiece and the sweep pass makes the
// removal obsolete — the sweep cancels it and keeps the bytes.
func TestSweep_RevivedClaimCancelsRemoval(t *testing.T) {
	w := setupRemovalTest(t)

	t.Run("acceptance revives", func(t *testing.T) {
		blob := mustMultihash(t, "blob-revived-accept")
		require.NoError(t, w.bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))
		require.NoError(t, w.svc.RemovePiece(t.Context(), blob))
		require.NoError(t, w.accepts.Put(t.Context(), acceptance.Acceptance{
			Space: testutil.RandomDID(t),
			Blob:  acceptance.Blob{Digest: blob, Size: 4},
			Cause: testutil.RandomCID(t),
		}))

		require.NoError(t, w.svc.processPendingRemovals(t.Context(), noopRemoveRoot))

		_, err := w.bs.Get(t.Context(), blob)
		require.NoError(t, err, "revived blob's bytes retained")
		require.Zero(t, w.count(t, "pdp_pending_piece_removals"),
			"obsolete removal cancelled")
	})

	t.Run("allocation revives", func(t *testing.T) {
		blob := mustMultihash(t, "blob-revived-alloc")
		require.NoError(t, w.bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))
		require.NoError(t, w.svc.RemovePiece(t.Context(), blob))
		require.NoError(t, w.allocs.Put(t.Context(), allocation.Allocation{
			Space: testutil.RandomDID(t),
			Blob:  blobcmd.Blob{Digest: blob, Size: 4},
			Cause: testutil.RandomCID(t),
		}))

		require.NoError(t, w.svc.processPendingRemovals(t.Context(), noopRemoveRoot))

		_, err := w.bs.Get(t.Context(), blob)
		require.NoError(t, err, "re-allocated blob's bytes retained")
		require.Zero(t, w.count(t, "pdp_pending_piece_removals"),
			"obsolete removal cancelled")
	})
}

// TestSweep_LiveSubPieceRetained: with the mh→commp row seeded exactly as
// CalculateCommP writes it (raw multihash bytes) and the piece live under a
// root with another live sub-piece, the sweep must retain the bytes and not
// retire the root.
func TestSweep_LiveSubPieceRetained(t *testing.T) {
	w := setupRemovalTest(t)
	blobA, blobB := mustMultihash(t, "blob-a"), mustMultihash(t, "blob-b")
	commpA2, commpA1 := testPiece(t, "piece-a")
	commpB2, commpB1 := testPiece(t, "piece-b")
	_, rootV1 := testPiece(t, "root-ab")
	require.NoError(t, w.bs.Put(t.Context(), blobA, 5, bytes.NewReader([]byte("dataA"))))
	require.NoError(t, w.bs.Put(t.Context(), blobB, 5, bytes.NewReader([]byte("dataB"))))
	w.seedMapping(t, blobA, commpA2, commpA1)
	w.seedMapping(t, blobB, commpB2, commpB1)
	refA := w.seedParkedChain(t, commpA2)
	refB := w.seedParkedChain(t, commpB2)
	w.seedPieceRow(t, 7, rootV1, commpA1, 0, refA)
	w.seedPieceRow(t, 7, rootV1, commpB1, 1024, refB)

	// Only blobA is pending removal: the root has a live sub-piece.
	require.NoError(t, w.svc.RemovePiece(t.Context(), blobA))

	calls := 0
	removeRoot := w.stampingRemoveRoot(t, &calls)
	require.NoError(t, w.svc.processPendingRemovals(t.Context(), removeRoot))

	require.Zero(t, calls, "root has a live sub-piece — not retired")
	_, err := w.bs.Get(t.Context(), blobA)
	require.NoError(t, err, "bytes retained while the root is being proven")
	require.Equal(t, 1, w.count(t, "pdp_pending_piece_removals"))

	// Releasing the last sub-piece triggers exactly one on-chain retirement.
	require.NoError(t, w.svc.RemovePiece(t.Context(), blobB))
	require.NoError(t, w.svc.processPendingRemovals(t.Context(), removeRoot))
	require.Equal(t, 1, calls)

	// Deletion tx in flight (rm_message_hash stamped) — nothing to finalize,
	// and the root must not be re-scheduled.
	require.NoError(t, w.svc.processPendingRemovals(t.Context(), removeRoot))
	require.Equal(t, 1, calls)
	_, err = w.bs.Get(t.Context(), blobA)
	require.NoError(t, err, "bytes retained until the deletion is confirmed")
}

// TestSweep_FinalizesAfterRemovalConfirmed: once NextProvingPeriod confirms
// the deletion (removed=TRUE), the sweep finalizes — bytes, parked chain,
// mapping, piece rows, and the pending row all released.
func TestSweep_FinalizesAfterRemovalConfirmed(t *testing.T) {
	w := setupRemovalTest(t)
	blob := mustMultihash(t, "blob-confirmed")
	commpV2, commpV1 := testPiece(t, "piece-confirmed")
	_, rootV1 := testPiece(t, "root-confirmed")
	require.NoError(t, w.bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))
	w.seedMapping(t, blob, commpV2, commpV1)
	ref := w.seedParkedChain(t, commpV2)
	w.seedPieceRow(t, 7, rootV1, commpV1, 0, ref)
	_, err := w.db.Exec(t.Context(), `
		UPDATE pdp_data_set_pieces SET rm_message_hash = '0xdead', removed = TRUE
	`)
	require.NoError(t, err)
	require.NoError(t, w.svc.RemovePiece(t.Context(), blob))

	require.NoError(t, w.svc.processPendingRemovals(t.Context(), noopRemoveRoot))

	_, err = w.bs.Get(t.Context(), blob)
	require.ErrorIs(t, err, store.ErrNotFound, "bytes released once the chain confirmed the removal")
	require.Zero(t, w.count(t, "pdp_data_set_pieces"))
	require.Zero(t, w.count(t, "pdp_piecerefs"))
	require.Zero(t, w.count(t, "parked_pieces"))
	require.Zero(t, w.count(t, "pdp_piece_mh_to_commp"))
	require.Zero(t, w.count(t, "pdp_pending_piece_removals"))
}

// TestSweep_FailedRemovalRescheduled: a failed deletion tx gets its
// rm_message_hash cleared by NextProvingPeriod; the next sweep pass
// re-schedules the retirement.
func TestSweep_FailedRemovalRescheduled(t *testing.T) {
	w := setupRemovalTest(t)
	blob := mustMultihash(t, "blob-retry")
	commpV2, commpV1 := testPiece(t, "piece-retry")
	_, rootV1 := testPiece(t, "root-retry")
	require.NoError(t, w.bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))
	w.seedMapping(t, blob, commpV2, commpV1)
	ref := w.seedParkedChain(t, commpV2)
	w.seedPieceRow(t, 7, rootV1, commpV1, 0, ref)
	require.NoError(t, w.svc.RemovePiece(t.Context(), blob))

	calls := 0
	removeRoot := w.stampingRemoveRoot(t, &calls)
	require.NoError(t, w.svc.processPendingRemovals(t.Context(), removeRoot))
	require.Equal(t, 1, calls)

	// The tx failed: NextProvingPeriod clears the stamp.
	_, err := w.db.Exec(t.Context(), `UPDATE pdp_data_set_pieces SET rm_message_hash = NULL`)
	require.NoError(t, err)

	require.NoError(t, w.svc.processPendingRemovals(t.Context(), removeRoot))
	require.Equal(t, 2, calls, "cleared stamp reschedules the retirement")
}

// TestSweep_OrphanedPieceRefsFinalize: piece refs without a pipeline row are
// dead bookkeeping from a cancelled row, not in-flight aggregation — they
// must not block finalization forever.
func TestSweep_OrphanedPieceRefsFinalize(t *testing.T) {
	w := setupRemovalTest(t)
	blob := mustMultihash(t, "blob-orphanrefs")
	commpV2, commpV1 := testPiece(t, "piece-orphanrefs")
	require.NoError(t, w.bs.Put(t.Context(), blob, 4, bytes.NewReader([]byte("data"))))
	w.seedMapping(t, blob, commpV2, commpV1)
	w.seedParkedChain(t, commpV2)
	require.NoError(t, w.svc.RemovePiece(t.Context(), blob))

	require.NoError(t, w.svc.processPendingRemovals(t.Context(), noopRemoveRoot))

	_, err := w.bs.Get(t.Context(), blob)
	require.ErrorIs(t, err, store.ErrNotFound, "orphaned refs don't block finalization")
	require.Zero(t, w.count(t, "pdp_piecerefs"), "orphaned piece refs cleaned up")
}
