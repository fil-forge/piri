package service

import (
	"testing"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/pdp/types"
)

// Curio's add-piece watcher marks a data set ready to prove once it has
// parsed the add's receipt and written its piece rows. Until then the data
// set has nothing to prove, so a receipt the watcher cannot parse must leave
// it unproven rather than proving, and faulting, with no pieces.
func TestRecordPieceAdds_LeavesDataSetNotReadyToProveUntilPiecesAreRecorded(t *testing.T) {
	w := setupRemovalTest(t)
	ctx := t.Context()
	commpV2, _ := testPiece(t, "any-piece")
	pieceRef := w.seedParkedChain(t, commpV2)
	w.seedAddMessage(t)
	piece := cid.MustParse(commpV2)

	_, err := w.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		return true, recordPieceAdds(tx, 1, "0xany-add",
			[]types.RootAdd{{Root: piece, SubRoots: []cid.Cid{piece}}},
			map[cid.Cid]*subrootPiece{piece: {
				PieceInfo:     abi.PieceInfo{Size: 1024, PieceCID: piece},
				PDPPieceRefID: pieceRef,
			}})
	})
	require.NoError(t, err)

	type dataSetState struct {
		InitReady   bool `db:"init_ready"`
		PendingAdds int  `db:"pending_adds"`
		Pieces      int  `db:"pieces"`
	}
	var got []dataSetState
	require.NoError(t, w.db.Select(ctx, &got, `
		SELECT init_ready,
		       (SELECT count(*) FROM pdp_data_set_piece_adds WHERE data_set = 1 AND NOT pieces_added) AS pending_adds,
		       (SELECT count(*) FROM pdp_data_set_pieces WHERE data_set = 1) AS pieces
		FROM pdp_data_sets WHERE id = 1`))
	require.Equal(t, []dataSetState{{InitReady: false, PendingAdds: 1, Pieces: 0}}, got)
}
