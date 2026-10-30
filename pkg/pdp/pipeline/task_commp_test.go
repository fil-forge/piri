package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/aggregator"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
	"github.com/fil-forge/piri/pkg/pdp/types"
)

// fakePieceAPI computes one fixed commP and records every piece it parks.
// The embedded interface is nil: the commP task calls nothing else.
type fakePieceAPI struct {
	types.PieceAPI
	commp  types.CalculateCommPResponse
	parked []types.ParkPieceRequest
}

func (f *fakePieceAPI) CalculateCommP(context.Context, multihash.Multihash) (types.CalculateCommPResponse, error) {
	return f.commp, nil
}

func (f *fakePieceAPI) ParkPiece(_ context.Context, req types.ParkPieceRequest) error {
	f.parked = append(f.parked, req)
	return nil
}

func TestCommPTask_DoesNotParkAPieceAlreadyParkedUnderItsV1CID(t *testing.T) {
	db := testutil.NewHarmonyDB(t)
	ctx := t.Context()
	blob, v2, v1 := testBlobAndPiece(t, "any-blob")
	// The bytes are still parked from an earlier pass, as ParkPiece left them.
	_, err := db.Exec(ctx, `INSERT INTO pdp_services (pubkey, service_label) VALUES ($1, 'storacha')`, []byte{1})
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		WITH piece AS (
			INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, complete)
			VALUES ($1, 1024, 1000, TRUE, TRUE) RETURNING id
		), ref AS (
			INSERT INTO parked_piece_refs (piece_id, data_url, long_term, data_headers)
			SELECT id, 'pdpstore://any-blob', TRUE, '{}'::jsonb FROM piece RETURNING ref_id
		)
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref) SELECT 'storacha', $1, ref_id FROM ref
	`, v1)
	require.NoError(t, err)
	const taskID = 1
	_, err = db.Exec(ctx, `INSERT INTO pdp_blob_pipeline (digest, commp_task_id) VALUES ($1, $2)`, []byte(blob), taskID)
	require.NoError(t, err)

	api := &fakePieceAPI{commp: types.CalculateCommPResponse{PieceCID: cid.MustParse(v2), RawSize: 1000, PaddedSize: 1024}}
	agg := NewAggregateTask(db, &recordingSubmitter{}, aggregator.NewPolicy(func() uint64 { return 1 << 40 }))
	agg.Adder(fakeAdder(t, db))
	task := NewCommPTask(db, api, agg, commp.NewPolicy(func() time.Duration { return 0 }))

	done, err := task.Do(ctx, taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Empty(t, api.parked)
}
