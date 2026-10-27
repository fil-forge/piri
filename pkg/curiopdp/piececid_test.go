package curiopdp

import (
	"context"
	"testing"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/internal/testutil"
)

// parkedPieceCIDs returns parked_pieces.piece_cid and pdp_piecerefs.piece_cid
// for the piece ref.
func parkedPieceCIDs(t *testing.T, db *harmonydb.DB, pieceRefID int64) []string {
	t.Helper()
	var parked, ref string
	require.NoError(t, db.QueryRow(context.Background(), `
		SELECT pp.piece_cid, pr.piece_cid
		FROM pdp_piecerefs pr
		JOIN parked_piece_refs pprf ON pprf.ref_id = pr.piece_ref
		JOIN parked_pieces pp ON pp.id = pprf.piece_id
		WHERE pr.id = $1`, pieceRefID).Scan(&parked, &ref))
	return []string{parked, ref}
}

func TestConvertParkedPiecesToV1_RewritesAPieceParkedUnderItsV2CID(t *testing.T) {
	db := testutil.NewHarmonyDB(t)
	v1, v2 := anyPiece(t, 1, 1000)
	pieceRefID := parkPiece(t, db, v2, 1000) // as ParkPiece wrote pieces before

	require.NoError(t, convertParkedPiecesToV1(t.Context(), db))

	require.Equal(t, []string{v1.String(), v1.String()}, parkedPieceCIDs(t, db, pieceRefID))
}

func TestConvertParkedPiecesToV1_LeavesAPieceParkedUnderItsV1CID(t *testing.T) {
	db := testutil.NewHarmonyDB(t)
	v1, _ := anyPiece(t, 1, 1000)
	pieceRefID := parkPiece(t, db, v1, 1000)

	require.NoError(t, convertParkedPiecesToV1(t.Context(), db))

	require.Equal(t, []string{v1.String(), v1.String()}, parkedPieceCIDs(t, db, pieceRefID))
}

func TestConvertParkedPiecesToV1_RewritesEveryPieceParkedUnderItsV2CID(t *testing.T) {
	db := testutil.NewHarmonyDB(t)
	var refs []int64
	var want []cid.Cid
	for seed := byte(1); seed <= 3; seed++ {
		v1, v2 := anyPiece(t, seed, 1000)
		refs = append(refs, parkPiece(t, db, v2, 1000))
		want = append(want, v1)
	}

	require.NoError(t, convertParkedPiecesToV1(t.Context(), db))

	for i, ref := range refs {
		require.Equal(t, []string{want[i].String(), want[i].String()}, parkedPieceCIDs(t, db, ref), "piece %d", i)
	}
}
