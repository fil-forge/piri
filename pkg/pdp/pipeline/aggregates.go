package pipeline

import (
	"bytes"
	"context"
	"fmt"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/ipfs/go-cid"

	aggtypes "github.com/fil-forge/piri/pkg/pdp/aggregation/types"
)

// An aggregate is stored on the pipeline rows of its pieces: aggregate_root
// names it, agg_position is the piece's index in Aggregate.Pieces, and
// inclusion_proof is the piece's CBOR-encoded proof. The rows, and the
// aggregate with them, are deleted when AddRoots retires the batch.

// storeAggregate folds the rows of digests into a. digests[i] is the blob
// whose piece is a.Pieces[i]; the row is matched by digest because two
// blobs can share a piece.
func storeAggregate(tx *harmonydb.Tx, a aggtypes.Aggregate, digests [][]byte) error {
	if len(digests) != len(a.Pieces) {
		return fmt.Errorf("aggregate %s has %d pieces but %d blobs", a.Root, len(a.Pieces), len(digests))
	}
	for i, p := range a.Pieces {
		var proof bytes.Buffer
		if err := p.InclusionProof.MarshalCBOR(&proof); err != nil {
			return fmt.Errorf("encoding inclusion proof of piece %s: %w", p.Link, err)
		}
		n, err := tx.Exec(`
			UPDATE pdp_blob_pipeline
			SET aggregate_root = $1, aggregated_at = now(), agg_position = $2, inclusion_proof = $3
			WHERE digest = $4 AND commp = $5 AND aggregate_root IS NULL
		`, a.Root.String(), i, proof.Bytes(), digests[i], p.Link.String())
		if err != nil {
			return fmt.Errorf("marking piece %s aggregated: %w", p.Link, err)
		}
		if n != 1 {
			return fmt.Errorf("marking piece %s aggregated: %d unaggregated rows match", p.Link, n)
		}
	}
	return nil
}

// loadAggregate rebuilds the aggregate root from its pipeline rows.
func loadAggregate(ctx context.Context, db *harmonydb.DB, root cid.Cid) (aggtypes.Aggregate, error) {
	var rows []struct {
		Commp          string `db:"commp"`
		AggPosition    *int   `db:"agg_position"`
		InclusionProof []byte `db:"inclusion_proof"`
	}
	if err := db.Select(ctx, &rows, `
		SELECT commp, agg_position, inclusion_proof FROM pdp_blob_pipeline
		WHERE aggregate_root = $1
		ORDER BY agg_position
	`, root.String()); err != nil {
		return aggtypes.Aggregate{}, fmt.Errorf("loading pieces of aggregate %s: %w", root, err)
	}
	if len(rows) == 0 {
		return aggtypes.Aggregate{}, fmt.Errorf("aggregate %s has no pieces", root)
	}

	a := aggtypes.Aggregate{Root: root, Pieces: make([]aggtypes.AggregatePiece, len(rows))}
	for i, r := range rows {
		if r.AggPosition == nil || *r.AggPosition != i || r.InclusionProof == nil {
			return aggtypes.Aggregate{}, fmt.Errorf("aggregate %s: piece %s is not stored at position %d", root, r.Commp, i)
		}
		link, err := cid.Parse(r.Commp)
		if err != nil {
			return aggtypes.Aggregate{}, fmt.Errorf("parsing piece cid %s: %w", r.Commp, err)
		}
		a.Pieces[i].Link = link
		if err := a.Pieces[i].InclusionProof.UnmarshalCBOR(bytes.NewReader(r.InclusionProof)); err != nil {
			return aggtypes.Aggregate{}, fmt.Errorf("decoding inclusion proof of piece %s: %w", r.Commp, err)
		}
	}
	return a, nil
}
