package service

import (
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
