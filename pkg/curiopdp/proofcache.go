package curiopdp

import (
	"context"
	"fmt"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/tasks/pdpv0"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/ipfs/go-cid"
)

// proofCacheStore keeps pdpv0's proof cache layers in pdp_proof_cache_layers,
// one row per layer with its nodes concatenated in index order.
type proofCacheStore struct {
	db *harmonydb.DB
}

var _ pdpv0.ProofCacheStore = (*proofCacheStore)(nil)

func newProofCacheStore(db *harmonydb.DB) *proofCacheStore {
	return &proofCacheStore{db: db}
}

func (s *proofCacheStore) AddPDPLayer(ctx context.Context, pieceCidV2 cid.Cid, layer []indexstore.NodeDigest) error {
	if len(layer) == 0 {
		return fmt.Errorf("caching an empty layer for piece %s", pieceCidV2)
	}
	layerIdx := layer[0].Layer
	nodes := make([]byte, 0, len(layer)*proof.NODE_SIZE)
	for i, node := range layer {
		if node.Layer != layerIdx || node.Index != int64(i) {
			return fmt.Errorf("node %d of the layer is layer %d index %d, want layer %d index %d", i, node.Layer, node.Index, layerIdx, i)
		}
		nodes = append(nodes, node.Hash[:]...)
	}
	pieceCidV1, _, err := commcid.PieceCidV1FromV2(pieceCidV2)
	if err != nil {
		return fmt.Errorf("decoding piece %s: %w", pieceCidV2, err)
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO pdp_proof_cache_layers (piece_cid_v2, piece_cid, layer_index, nodes)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (piece_cid_v2, layer_index) DO UPDATE SET nodes = EXCLUDED.nodes`,
		pieceCidV2.Bytes(), pieceCidV1.String(), layerIdx, nodes); err != nil {
		return fmt.Errorf("saving layer %d of piece %s: %w", layerIdx, pieceCidV2, err)
	}
	return nil
}

func (s *proofCacheStore) GetPDPLayerIndex(ctx context.Context, pieceCidV2 cid.Cid) (bool, int, error) {
	var rows []int
	if err := s.db.Select(ctx, &rows, `
		SELECT layer_index FROM pdp_proof_cache_layers
		WHERE piece_cid_v2 = $1 ORDER BY layer_index LIMIT 1`, pieceCidV2.Bytes()); err != nil {
		return false, 0, fmt.Errorf("finding the cached layer of piece %s: %w", pieceCidV2, err)
	}
	if len(rows) == 0 {
		return false, 0, nil
	}
	return true, rows[0], nil
}

// GetPDPLayer returns no nodes, and no error, for a layer that is not cached.
func (s *proofCacheStore) GetPDPLayer(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int) ([]indexstore.NodeDigest, error) {
	var rows [][]byte
	if err := s.db.Select(ctx, &rows, `
		SELECT nodes FROM pdp_proof_cache_layers
		WHERE piece_cid_v2 = $1 AND layer_index = $2`, pieceCidV2.Bytes(), layerIdx); err != nil {
		return nil, fmt.Errorf("reading layer %d of piece %s: %w", layerIdx, pieceCidV2, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	nodes := rows[0]
	if len(nodes)%proof.NODE_SIZE != 0 {
		return nil, fmt.Errorf("layer %d of piece %s is %d bytes, not a whole number of nodes", layerIdx, pieceCidV2, len(nodes))
	}
	layer := make([]indexstore.NodeDigest, len(nodes)/proof.NODE_SIZE)
	for i := range layer {
		layer[i] = indexstore.NodeDigest{
			Layer: layerIdx,
			Index: int64(i),
			Hash:  [32]byte(nodes[i*proof.NODE_SIZE:]),
		}
	}
	return layer, nil
}

func (s *proofCacheStore) GetPDPNode(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int, index int64) (bool, *indexstore.NodeDigest, error) {
	var rows [][]byte
	// substring counts bytes from 1, and yields fewer than 32 past the layer's end.
	if err := s.db.Select(ctx, &rows, `
		SELECT substring(nodes FROM $3::integer * 32 + 1 FOR 32) FROM pdp_proof_cache_layers
		WHERE piece_cid_v2 = $1 AND layer_index = $2`, pieceCidV2.Bytes(), layerIdx, index); err != nil {
		return false, nil, fmt.Errorf("reading node %d of layer %d of piece %s: %w", index, layerIdx, pieceCidV2, err)
	}
	if len(rows) == 0 || len(rows[0]) != proof.NODE_SIZE {
		return false, nil, nil
	}
	return true, &indexstore.NodeDigest{Layer: layerIdx, Index: index, Hash: [32]byte(rows[0])}, nil
}
