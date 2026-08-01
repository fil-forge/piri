package pipeline

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/internal/testutil"
)

// fakeAdder wires a CommPTask's promise to a synchronous AddTask stand-in:
// the extra func runs in a real transaction against a fake task id, exactly
// as harmonytask's AddTask would, minus the harmony_task insert.
func fakeAdder(t *testing.T, db *harmonydb.DB) harmonytask.AddTaskFunc {
	var seq harmonytask.TaskID
	return func(extra func(harmonytask.TaskID, *harmonydb.Tx) (bool, error)) {
		seq++
		_, err := db.BeginTransaction(context.Background(), func(tx *harmonydb.Tx) (bool, error) {
			return extra(seq, tx)
		})
		require.NoError(t, err)
	}
}

func newTestEntry(t *testing.T) (*Entry, *harmonydb.DB) {
	t.Helper()
	db := testutil.NewHarmonyDB(t)
	task := NewCommPTask(db, nil, nil)
	task.Adder(fakeAdder(t, db))
	return NewEntry(db, task), db
}

func testBlobAndPiece(t *testing.T, seed string) (multihash.Multihash, string, string) {
	t.Helper()
	blob, err := multihash.Sum([]byte(seed), multihash.SHA2_256, -1)
	require.NoError(t, err)
	digest := sha256.Sum256([]byte("piece-" + seed))
	digest[31] &= 0b00111111
	v1, err := commcid.DataCommitmentV1ToCID(digest[:])
	require.NoError(t, err)
	v2, err := commcid.PieceCidV2FromV1(v1, 1024)
	require.NoError(t, err)
	return blob, v2.String(), v1.String()
}

func pipelineRows(t *testing.T, db *harmonydb.DB) (n int, claimed int) {
	t.Helper()
	require.NoError(t, db.QueryRow(context.Background(),
		`SELECT count(*), count(commp_task_id) FROM pdp_blob_pipeline`).Scan(&n, &claimed))
	return n, claimed
}

// TestEnqueue_InsertsAndClaims: a fresh blob gets a pipeline row claimed by
// a commp task; re-enqueueing the same blob (a second space accepting the
// same content) is a no-op.
func TestEnqueue_InsertsAndClaims(t *testing.T) {
	e, db := newTestEntry(t)
	blob, _, _ := testBlobAndPiece(t, "blob-fresh")

	require.NoError(t, e.Enqueue(t.Context(), blob))
	n, claimed := pipelineRows(t, db)
	require.Equal(t, 1, n)
	require.Equal(t, 1, claimed, "row claimed by a spawned commp task")

	require.NoError(t, e.Enqueue(t.Context(), blob))
	n, _ = pipelineRows(t, db)
	require.Equal(t, 1, n, "re-enqueue dedups on the pipeline row")
}

// TestEnqueue_SkipsLiveContent: content whose piece is live on-chain (or
// staged to be) is a re-accept the pipeline has already carried through —
// no new pipeline entry.
func TestEnqueue_SkipsLiveContent(t *testing.T) {
	e, db := newTestEntry(t)
	blob, v2, v1 := testBlobAndPiece(t, "blob-live")
	ctx := t.Context()

	seedLivePiece(t, db, blob, v2, v1, nil, false)

	require.NoError(t, e.Enqueue(ctx, blob))
	n, _ := pipelineRows(t, db)
	require.Zero(t, n, "live content needs no pipeline entry")
}

// TestEnqueue_ReenqueuesRemovalScheduledContent pins the revival hole from
// the removal design: a piece whose deletion tx is in flight
// (rm_message_hash set) is leaving the proof set, so a racing re-accept of
// the same content MUST ride the pipeline again under a new root — skipping
// it would leave claims on content that is no longer proven.
func TestEnqueue_ReenqueuesRemovalScheduledContent(t *testing.T) {
	e, db := newTestEntry(t)
	blob, v2, v1 := testBlobAndPiece(t, "blob-rm-scheduled")
	rm := "0xdead"

	seedLivePiece(t, db, blob, v2, v1, &rm, false)

	require.NoError(t, e.Enqueue(t.Context(), blob))
	n, claimed := pipelineRows(t, db)
	require.Equal(t, 1, n, "removal-scheduled content re-enters the pipeline")
	require.Equal(t, 1, claimed)
}

// seedLivePiece seeds the mapping plus a pdp_data_set_pieces row (with its
// FK chain) for the blob's piece.
func seedLivePiece(t *testing.T, db *harmonydb.DB, blob multihash.Multihash, v2, v1 string, rmHash *string, removed bool) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Exec(ctx, `
		INSERT INTO pdp_piece_mh_to_commp (mhash, size, commp, commp_v1)
		VALUES ($1, 4, $2, $3)
	`, []byte(blob), v2, v1)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
		VALUES ('0xadd', 'confirmed') ON CONFLICT DO NOTHING
	`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_services (id, pubkey, service_label)
		VALUES (1, $1, 'storacha') ON CONFLICT DO NOTHING
	`, []byte{1})
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_sets (id, create_message_hash, service)
		VALUES (1, '0xadd', 'storacha') ON CONFLICT DO NOTHING
	`)
	require.NoError(t, err)

	var pieceID int64
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, complete)
		VALUES ($1, 1024, 1000, TRUE, TRUE) RETURNING id
	`, v2).Scan(&pieceID))
	var refID int64
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO parked_piece_refs (piece_id, data_url, long_term, data_headers)
		VALUES ($1, 'pdpstore://x', TRUE, '{}'::jsonb) RETURNING ref_id
	`, pieceID).Scan(&refID))
	var pieceRefID int64
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref)
		VALUES ('storacha', $1, $2) RETURNING id
	`, v2, refID).Scan(&pieceRefID))

	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_set_pieces
			(data_set, piece, add_message_hash, add_message_index, piece_id,
			 sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref,
			 rm_message_hash, removed)
		VALUES (1, $1, '0xadd', 0, 7, $2, 0, 1024, $3, $4, $5)
	`, v1, v1, pieceRefID, rmHash, removed)
	require.NoError(t, err)
}
