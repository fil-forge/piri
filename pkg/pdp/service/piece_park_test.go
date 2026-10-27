package service

import (
	"testing"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/pdp/types"
)

func newParkingService(t *testing.T) (*PDPService, *harmonydb.DB) {
	t.Helper()
	db := piritestutil.NewHarmonyDB(t)
	_, err := db.Exec(t.Context(), `
		INSERT INTO pdp_services (pubkey, service_label) VALUES ($1, 'storacha'), ($2, 'any-other-service')
	`, []byte{1}, []byte{2})
	require.NoError(t, err)
	return &PDPService{db: db, name: "storacha"}, db
}

func parkTestPiece(t *testing.T, svc *PDPService, seed string) (v2, v1 cid.Cid) {
	t.Helper()
	v2s, v1s := testPiece(t, seed)
	v2, v1 = cid.MustParse(v2s), cid.MustParse(v1s)
	require.NoError(t, svc.ParkPiece(t.Context(), types.ParkPieceRequest{
		Blob:       mustMultihash(t, seed),
		PieceCID:   v2,
		RawSize:    1000,
		PaddedSize: 1024,
	}))
	return v2, v1
}

func TestParkPiece_RecordsThePieceUnderItsV1CID(t *testing.T) {
	svc, db := newParkingService(t)

	_, v1 := parkTestPiece(t, svc, "any-piece")

	var parked, ref string
	require.NoError(t, db.QueryRow(t.Context(), `
		SELECT pp.piece_cid, pr.piece_cid
		FROM pdp_piecerefs pr
		JOIN parked_piece_refs pprf ON pprf.ref_id = pr.piece_ref
		JOIN parked_pieces pp ON pp.id = pprf.piece_id
	`).Scan(&parked, &ref))
	require.Equal(t, []string{v1.String(), v1.String()}, []string{parked, ref})
}

func TestFindParkedSubroots_FindsAParkedPieceByTheCIDTheRequestNames(t *testing.T) {
	svc, db := newParkingService(t)
	v2, _ := parkTestPiece(t, svc, "any-piece")
	var pieceRefID int64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT id FROM pdp_piecerefs`).Scan(&pieceRefID))

	found, err := svc.findParkedSubroots(t.Context(), []cid.Cid{v2})
	require.NoError(t, err)
	require.Equal(t, map[cid.Cid]parkedSubroot{
		v2: {PDPPieceRefID: pieceRefID, PaddedSize: 1024, RawSize: 1000},
	}, found)
}

func TestFindParkedSubroots_LeavesOutAPieceNotParked(t *testing.T) {
	svc, _ := newParkingService(t)
	parked, _ := parkTestPiece(t, svc, "any-piece")
	notParkedV2, _ := testPiece(t, "another-piece")

	found, err := svc.findParkedSubroots(t.Context(), []cid.Cid{parked, cid.MustParse(notParkedV2)})
	require.NoError(t, err)
	require.Equal(t, []cid.Cid{parked}, keysOf(found))
}

func TestFindParkedSubroots_LeavesOutAPieceParkedForAnotherService(t *testing.T) {
	svc, db := newParkingService(t)
	v2, _ := parkTestPiece(t, svc, "any-piece")
	_, err := db.Exec(t.Context(), `UPDATE pdp_piecerefs SET service = 'any-other-service'`)
	require.NoError(t, err)

	found, err := svc.findParkedSubroots(t.Context(), []cid.Cid{v2})
	require.NoError(t, err)
	require.Empty(t, found)
}

func keysOf(m map[cid.Cid]parkedSubroot) []cid.Cid {
	keys := make([]cid.Cid, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
