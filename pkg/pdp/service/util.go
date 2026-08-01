package service

import (
	"fmt"

	"github.com/fil-forge/piri/pkg/pdp/proof"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multicodec"
)

var PieceSizeLimit = abi.PaddedPieceSize(proof.MaxMemtreeSize).Unpadded()

// asPieceCIDv1 normalizes a piece CID to its v1 (raw CommP) form.
// v2 piece CIDs (fr32-sha256-trunc254-padbintree) are converted via
// commcid.PieceCidV1FromV2; v1 CIDs pass through unchanged.
func asPieceCIDv1(pieceCid cid.Cid) (cid.Cid, error) {
	if pieceCid.Prefix().MhType == uint64(multicodec.Fr32Sha256Trunc254Padbintree) {
		c1, _, err := commcid.PieceCidV1FromV2(pieceCid)
		return c1, err
	}
	return pieceCid, nil
}

// pieceCIDv1String derives the v1 CommP string for a v2 piece CID string —
// the form the pdpv0 tables (pdp_data_set_pieces, pdp_data_set_piece_adds)
// are keyed by. v1 is derivable from v2, never the reverse (v1 lacks the
// size), which is why piri stores only the v2 form and converts at the
// pdpv0 boundary.
func pieceCIDv1String(commp string) (string, error) {
	c, err := cid.Parse(commp)
	if err != nil {
		return "", fmt.Errorf("parsing piece cid %s: %w", commp, err)
	}
	v1, err := asPieceCIDv1(c)
	if err != nil {
		return "", fmt.Errorf("deriving v1 piece cid from %s: %w", commp, err)
	}
	return v1.String(), nil
}
