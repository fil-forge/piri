package curiopdp

import (
	"bytes"
	"context"
	"math/rand"
	"testing"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/tasks/pdpv0"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/internal/testutil"
)

// anyPiece is a piece of rawSize bytes that tests can cache layers for without
// reading its data.
func anyPiece(t *testing.T, seed byte, rawSize uint64) (v1, v2 cid.Cid) {
	t.Helper()
	commitment := bytes.Repeat([]byte{seed}, 32)
	commitment[31] &= 0x3f // a valid commitment is a field element
	v1, err := commcid.DataCommitmentV1ToCID(commitment)
	require.NoError(t, err)
	v2, err = commcid.PieceCidV2FromV1(v1, rawSize)
	require.NoError(t, err)
	return v1, v2
}

func layerOf(layer int, hashes ...byte) []indexstore.NodeDigest {
	nodes := make([]indexstore.NodeDigest, len(hashes))
	for i, h := range hashes {
		nodes[i] = indexstore.NodeDigest{Layer: layer, Index: int64(i), Hash: [32]byte{h}}
	}
	return nodes
}

// parkPiece inserts the parked_pieces, parked_piece_refs and pdp_piecerefs rows
// ParkPiece writes for a piece, and returns the pdp_piecerefs id.
func parkPiece(t *testing.T, db *harmonydb.DB, pieceCid cid.Cid, rawSize uint64) int64 {
	t.Helper()
	ctx := context.Background()
	var pieceID, refID, pieceRefID int64
	_, err := db.Exec(ctx, `
		INSERT INTO pdp_services (pubkey, service_label) VALUES ($1, 'any-service')
		ON CONFLICT DO NOTHING`, []byte{1})
	require.NoError(t, err)
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, complete)
		VALUES ($1, $2, $3, TRUE, TRUE) RETURNING id`,
		pieceCid.String(), int64(rawSize), int64(rawSize)).Scan(&pieceID))
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO parked_piece_refs (piece_id, data_url, long_term, data_headers)
		VALUES ($1, 'pdpstore://any-blob', TRUE, '{}'::jsonb) RETURNING ref_id`,
		pieceID).Scan(&refID))
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref) VALUES ('any-service', $1, $2) RETURNING id`,
		pieceCid.String(), refID).Scan(&pieceRefID))
	return pieceRefID
}

func TestProofCacheStore_ReturnsTheLayerItWasGiven(t *testing.T) {
	ctx := context.Background()
	store := newProofCacheStore(testutil.NewHarmonyDB(t))
	_, v2 := anyPiece(t, 1, 1<<20)
	layer := layerOf(7, 0xa0, 0xa1, 0xa2)

	require.NoError(t, store.AddPDPLayer(ctx, v2, layer))

	got, err := store.GetPDPLayer(ctx, v2, 7)
	require.NoError(t, err)
	require.Equal(t, layer, got)
}

func TestProofCacheStore_ReportsTheIndexOfTheCachedLayer(t *testing.T) {
	ctx := context.Background()
	store := newProofCacheStore(testutil.NewHarmonyDB(t))
	_, v2 := anyPiece(t, 1, 1<<20)
	require.NoError(t, store.AddPDPLayer(ctx, v2, layerOf(7, 0xa0)))

	has, layerIdx, err := store.GetPDPLayerIndex(ctx, v2)
	require.NoError(t, err)
	require.Equal(t, []any{true, 7}, []any{has, layerIdx})
}

func TestProofCacheStore_ReadsASingleNodeByIndex(t *testing.T) {
	ctx := context.Background()
	store := newProofCacheStore(testutil.NewHarmonyDB(t))
	_, v2 := anyPiece(t, 1, 1<<20)
	require.NoError(t, store.AddPDPLayer(ctx, v2, layerOf(7, 0xa0, 0xa1, 0xa2)))

	has, node, err := store.GetPDPNode(ctx, v2, 7, 2)
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, &indexstore.NodeDigest{Layer: 7, Index: 2, Hash: [32]byte{0xa2}}, node)
}

func TestProofCacheStore_FindsNothingForAPieceWithNoLayer(t *testing.T) {
	ctx := context.Background()
	store := newProofCacheStore(testutil.NewHarmonyDB(t))
	_, cached := anyPiece(t, 1, 1<<20)
	_, uncached := anyPiece(t, 2, 1<<20)
	require.NoError(t, store.AddPDPLayer(ctx, cached, layerOf(7, 0xa0)))

	hasLayer, _, err := store.GetPDPLayerIndex(ctx, uncached)
	require.NoError(t, err)
	layer, err := store.GetPDPLayer(ctx, uncached, 7)
	require.NoError(t, err)
	hasNode, node, err := store.GetPDPNode(ctx, uncached, 7, 0)
	require.NoError(t, err)

	require.Equal(t, []any{false, []indexstore.NodeDigest(nil), false, (*indexstore.NodeDigest)(nil)},
		[]any{hasLayer, layer, hasNode, node})
}

func TestProofCacheStore_FindsNoNodePastTheEndOfTheLayer(t *testing.T) {
	ctx := context.Background()
	store := newProofCacheStore(testutil.NewHarmonyDB(t))
	_, v2 := anyPiece(t, 1, 1<<20)
	require.NoError(t, store.AddPDPLayer(ctx, v2, layerOf(7, 0xa0, 0xa1)))

	has, node, err := store.GetPDPNode(ctx, v2, 7, 2)
	require.NoError(t, err)
	require.Equal(t, []any{false, (*indexstore.NodeDigest)(nil)}, []any{has, node})
}

func TestProofCacheStore_ReplacesALayerSavedAgain(t *testing.T) {
	ctx := context.Background()
	store := newProofCacheStore(testutil.NewHarmonyDB(t))
	_, v2 := anyPiece(t, 1, 1<<20)
	require.NoError(t, store.AddPDPLayer(ctx, v2, layerOf(7, 0xa0, 0xa1)))

	require.NoError(t, store.AddPDPLayer(ctx, v2, layerOf(7, 0xb0, 0xb1)))

	got, err := store.GetPDPLayer(ctx, v2, 7)
	require.NoError(t, err)
	require.Equal(t, layerOf(7, 0xb0, 0xb1), got)
}

func TestProofCacheStore_RefusesNodesOutOfOrder(t *testing.T) {
	store := newProofCacheStore(testutil.NewHarmonyDB(t))
	_, v2 := anyPiece(t, 1, 1<<20)
	layer := layerOf(7, 0xa0, 0xa1)
	layer[0], layer[1] = layer[1], layer[0] // node 1 comes first

	err := store.AddPDPLayer(context.Background(), v2, layer)
	require.ErrorContains(t, err, "node 0 of the layer is layer 7 index 1")
}

func TestProofCacheStore_DropsAPiecesLayerWithItsLastRef(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewHarmonyDB(t)
	store := newProofCacheStore(db)
	v1, v2 := anyPiece(t, 1, 1<<20)
	parkPiece(t, db, v1, 1<<20)
	require.NoError(t, store.AddPDPLayer(ctx, v2, layerOf(7, 0xa0)))

	_, err := db.Exec(ctx, `DELETE FROM pdp_piecerefs WHERE piece_cid = $1`, v1.String())
	require.NoError(t, err)

	has, _, err := store.GetPDPLayerIndex(ctx, v2)
	require.NoError(t, err)
	require.False(t, has)
}

func TestProofCacheStore_KeepsAPiecesLayerWhileAnotherRefRemains(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewHarmonyDB(t)
	store := newProofCacheStore(db)
	v1, v2 := anyPiece(t, 1, 1<<20)
	first := parkPiece(t, db, v1, 1<<20)
	_, err := db.Exec(ctx, `
		WITH second_ref AS (
			INSERT INTO parked_piece_refs (piece_id, data_url, long_term, data_headers)
			SELECT pprf.piece_id, pprf.data_url, TRUE, '{}'::jsonb
			FROM pdp_piecerefs pr JOIN parked_piece_refs pprf ON pprf.ref_id = pr.piece_ref
			WHERE pr.id = $1
			RETURNING ref_id
		)
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref)
		SELECT 'any-service', $2, ref_id FROM second_ref`, first, v1.String())
	require.NoError(t, err)
	require.NoError(t, store.AddPDPLayer(ctx, v2, layerOf(7, 0xa0)))

	_, err = db.Exec(ctx, `DELETE FROM pdp_piecerefs WHERE id = $1`, first)
	require.NoError(t, err)

	has, _, err := store.GetPDPLayerIndex(ctx, v2)
	require.NoError(t, err)
	require.True(t, has)
}

// bytesPieceReader serves one piece's bytes for both of pdpv0's reader
// interfaces.
type bytesPieceReader struct{ data []byte }

type bytesReadCloser struct{ *bytes.Reader }

func (bytesReadCloser) Close() error { return nil }

func (r bytesPieceReader) GetSharedPieceReader(context.Context, cid.Cid, bool) (storiface.Reader, uint64, error) {
	return bytesReadCloser{bytes.NewReader(r.data)}, uint64(len(r.data)), nil
}

func (r bytesPieceReader) GetPieceReader(context.Context, cid.Cid) (proof.SectionReadCloser, uint64, error) {
	return bytesReadCloser{bytes.NewReader(r.data)}, uint64(len(r.data)), nil
}

// storeProofCache reads the store through proof.ProofCache, as pdpv0's prove
// task does.
type storeProofCache struct{ store pdpv0.ProofCacheStore }

func (c storeProofCache) GetLayerIndex(ctx context.Context, v2 cid.Cid) (bool, int, error) {
	return c.store.GetPDPLayerIndex(ctx, v2)
}

func (c storeProofCache) GetNode(ctx context.Context, v2 cid.Cid, layer int, index int64) (bool, [32]byte, error) {
	has, node, err := c.store.GetPDPNode(ctx, v2, layer, index)
	if err != nil || !has {
		return has, [32]byte{}, err
	}
	return true, node.Hash, nil
}

func (c storeProofCache) GetLayer(ctx context.Context, v2 cid.Cid, layer int) ([]byte, error) {
	nodes, err := c.store.GetPDPLayer(ctx, v2, layer)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(nodes)*proof.NODE_SIZE)
	for _, n := range nodes {
		out = append(out, n.Hash[:]...)
	}
	return out, nil
}

func TestProofCacheStore_ProvesAPieceAboveTheCacheThresholdFromTheLayerSaveCacheSaved(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewHarmonyDB(t)
	store := newProofCacheStore(db)

	// 40 MiB pads to 64 MiB, above the 32 MiB padded size Prove proves only
	// through the cache.
	data := make([]byte, 40<<20)
	rand.New(rand.NewSource(1)).Read(data)
	calc := &commp.Calc{}
	_, err := calc.Write(data)
	require.NoError(t, err)
	commitment, _, err := calc.Digest()
	require.NoError(t, err)
	v1, err := commcid.DataCommitmentV1ToCID(commitment)
	require.NoError(t, err)
	v2, err := commcid.PieceCidV2FromV1(v1, uint64(len(data)))
	require.NoError(t, err)

	pieceRefID := parkPiece(t, db, v1, uint64(len(data)))
	const taskID = 1
	_, err = db.Exec(ctx, `UPDATE pdp_piecerefs SET save_cache_task_id = $1 WHERE id = $2`, taskID, pieceRefID)
	require.NoError(t, err)
	reader := bytesPieceReader{data: data}
	done, err := pdpv0.NewTaskPDPSaveCache(db, reader, store).Do(ctx, taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)

	const challengedLeaf = 1<<20 + 12345 // in the 33rd MiB of the padded piece
	got, err := proof.GenerateCachedProof(ctx, reader, storeProofCache{store}, v2, challengedLeaf)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, proof.VerifyProof(got.Leaf, got.Proof, [32]byte(commitment), challengedLeaf))
}

// saveCacheTaskID returns pdp_piecerefs.save_cache_task_id for the piece ref,
// nil when no task holds it.
func saveCacheTaskID(t *testing.T, db *harmonydb.DB, pieceRefID int64) *int64 {
	t.Helper()
	var id *int64
	require.NoError(t, db.QueryRow(context.Background(),
		`SELECT save_cache_task_id FROM pdp_piecerefs WHERE id = $1`, pieceRefID).Scan(&id))
	return id
}

func TestReclaimOrphanedSaveCaches_FreesAPieceWhoseTaskTheEngineDeleted(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewHarmonyDB(t)
	v1, _ := anyPiece(t, 1, 1<<20)
	pieceRefID := parkPiece(t, db, v1, 1<<20)
	// Task 8 failed MaxFailures times; the engine deleted it from harmony_task.
	_, err := db.Exec(ctx, `UPDATE pdp_piecerefs SET save_cache_task_id = 8 WHERE id = $1`, pieceRefID)
	require.NoError(t, err)

	n, err := reclaimOrphanedSaveCaches(ctx, db)
	require.NoError(t, err)

	require.Equal(t, []any{1, (*int64)(nil)}, []any{n, saveCacheTaskID(t, db, pieceRefID)})
}

func TestReclaimOrphanedSaveCaches_KeepsAPieceWhoseTaskIsStillQueued(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewHarmonyDB(t)
	v1, _ := anyPiece(t, 1, 1<<20)
	pieceRefID := parkPiece(t, db, v1, 1<<20)
	_, err := db.Exec(ctx, `INSERT INTO harmony_task (id, posted_time, added_by, name) VALUES (7, now(), 1, 'PDPv0_SaveCache')`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE pdp_piecerefs SET save_cache_task_id = 7 WHERE id = $1`, pieceRefID)
	require.NoError(t, err)

	n, err := reclaimOrphanedSaveCaches(ctx, db)
	require.NoError(t, err)

	taskID := int64(7)
	require.Equal(t, []any{0, &taskID}, []any{n, saveCacheTaskID(t, db, pieceRefID)})
}
