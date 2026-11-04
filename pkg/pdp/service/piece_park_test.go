package service

import (
	"testing"

	"github.com/filecoin-project/curio/lib/proof"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/pdp/types"
)

// parkAndReadCacheFlag parks a piece of the given padded size and returns the
// needs_save_cache flag ParkPiece set on its pdp_piecerefs row.
func parkAndReadCacheFlag(t *testing.T, seed string, paddedSize int64) bool {
	t.Helper()
	ctx := t.Context()
	db := piritestutil.NewHarmonyDB(t)
	_, err := db.Exec(ctx, `
		INSERT INTO pdp_services (id, pubkey, service_label)
		VALUES (1, $1, 'storacha') ON CONFLICT DO NOTHING
	`, []byte{1})
	require.NoError(t, err)

	commpV2, _ := testPiece(t, seed)
	pieceCid, err := cid.Decode(commpV2)
	require.NoError(t, err)
	svc := &PDPService{db: db}
	require.NoError(t, svc.ParkPiece(ctx, types.ParkPieceRequest{
		Blob:       mustMultihash(t, seed),
		PieceCID:   pieceCid,
		RawSize:    paddedSize / 128 * 127,
		PaddedSize: paddedSize,
	}))

	var needsSaveCache bool
	require.NoError(t, db.QueryRow(ctx,
		`SELECT needs_save_cache FROM pdp_piecerefs WHERE piece_cid = $1`, commpV2).Scan(&needsSaveCache))
	return needsSaveCache
}

func TestParkPiece_FlagsAPieceAboveTheCacheThresholdForItsLayer(t *testing.T) {
	require.True(t, parkAndReadCacheFlag(t, "large", 2*int64(proof.MIN_PADDED_PIECE_SIZE_FOR_CACHE)))
}

// Curio proves a piece at or below the threshold from a full in-memory tree,
// so it never needs a layer.
func TestParkPiece_LeavesAPieceAtTheCacheThresholdUnflagged(t *testing.T) {
	require.False(t, parkAndReadCacheFlag(t, "small", int64(proof.MIN_PADDED_PIECE_SIZE_FOR_CACHE)))
}
