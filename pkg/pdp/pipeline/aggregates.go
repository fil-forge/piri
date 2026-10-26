package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"

	aggtypes "github.com/fil-forge/piri/pkg/pdp/aggregation/types"
)

// An aggregate is stored on the pipeline rows of its pieces: aggregate_root
// names it, agg_position is the piece's index in Aggregate.Pieces, and
// inclusion_proof is the piece's CBOR-encoded proof. The rows, and the
// aggregate with them, are deleted when AddRoots retires the batch.
//
// Blobs that share pieces can fold into the same aggregate more than once,
// for example two blobs whose piece is large enough to be an aggregate on
// its own. Every occurrence stores the same pieces, positions and proofs
// under the same root, and AddRoots submits the root once.

// storeAggregate folds the rows of digests into a. digests[i] is the blob
// whose piece is a.Pieces[i]; the row is matched by digest because two
// blobs can share a piece.
func storeAggregate(tx *harmonydb.Tx, a aggtypes.Aggregate, digests [][]byte) error {
	if len(digests) != len(a.Pieces) {
		return fmt.Errorf("aggregate %s has %d pieces but %d blobs", a.Root, len(a.Pieces), len(digests))
	}
	positionByDigest := make(map[string]int, len(digests))
	for i, d := range digests {
		if j, ok := positionByDigest[string(d)]; ok {
			return fmt.Errorf("aggregate %s: blob %s is given for the pieces at positions %d and %d", a.Root, multihash.Multihash(d), j, i)
		}
		positionByDigest[string(d)] = i
	}
	commps := make([]string, len(a.Pieces))
	positions := make([]int32, len(a.Pieces))
	proofs := make([][]byte, len(a.Pieces))
	for i, p := range a.Pieces {
		var proof bytes.Buffer
		if err := p.InclusionProof.MarshalCBOR(&proof); err != nil {
			return fmt.Errorf("encoding inclusion proof of piece %s: %w", p.Link, err)
		}
		commps[i] = p.Link.String()
		positions[i] = int32(i)
		proofs[i] = proof.Bytes()
	}
	// The digests are distinct and digest is the primary key, so each element
	// matches at most one row and the count is short exactly when some piece's
	// row is missing, holds another piece, or is already aggregated.
	n, err := tx.Exec(`
		UPDATE pdp_blob_pipeline p
		SET aggregate_root = $1, aggregated_at = now(), agg_position = v.position, inclusion_proof = v.proof
		FROM unnest($2::bytea[], $3::text[], $4::int[], $5::bytea[]) AS v(digest, commp, position, proof)
		WHERE p.digest = v.digest AND p.commp = v.commp AND p.aggregate_root IS NULL
	`, a.Root.String(), digests, commps, positions, proofs)
	if err != nil {
		return fmt.Errorf("marking pieces aggregated: %w", err)
	}
	if n != len(a.Pieces) {
		return unmatchedPiecesError(tx, a, digests, commps, n)
	}
	return nil
}

// unmatchedPiecesError explains a short count from storeAggregate's UPDATE.
// It runs in the same transaction, so the rows the UPDATE matched carry
// a.Root and every other element of digests is reported with its cause.
func unmatchedPiecesError(tx *harmonydb.Tx, a aggtypes.Aggregate, digests [][]byte, commps []string, matched int) error {
	var rows []struct {
		Position  int64   `db:"position"`
		Digest    []byte  `db:"digest"`
		WantCommp string  `db:"want_commp"`
		RowExists bool    `db:"row_exists"`
		RowCommp  *string `db:"row_commp"`
		RowRoot   *string `db:"row_root"`
	}
	if err := tx.Select(&rows, `
		SELECT v.ord - 1 AS position, v.digest, v.commp AS want_commp,
		       p.digest IS NOT NULL AS row_exists, p.commp AS row_commp, p.aggregate_root AS row_root
		FROM unnest($1::bytea[], $2::text[]) WITH ORDINALITY AS v(digest, commp, ord)
		LEFT JOIN pdp_blob_pipeline p ON p.digest = v.digest
		WHERE p.aggregate_root IS DISTINCT FROM $3
		ORDER BY v.ord
	`, digests, commps, a.Root.String()); err != nil {
		return fmt.Errorf("marking pieces aggregated: %d of %d pieces match an unaggregated row; finding the others: %w", matched, len(a.Pieces), err)
	}

	causes := make([]string, len(rows))
	for i, r := range rows {
		blob := multihash.Multihash(r.Digest)
		var cause string
		switch {
		case !r.RowExists:
			cause = fmt.Sprintf("blob %s has no pipeline row", blob)
		case r.RowCommp == nil:
			cause = fmt.Sprintf("blob %s has no commp", blob)
		case *r.RowCommp != r.WantCommp:
			cause = fmt.Sprintf("blob %s has piece %s", blob, *r.RowCommp)
		case r.RowRoot != nil:
			cause = fmt.Sprintf("blob %s is already in aggregate %s", blob, *r.RowRoot)
		default:
			cause = fmt.Sprintf("blob %s was not updated", blob)
		}
		causes[i] = fmt.Sprintf("position %d (piece %s): %s", r.Position, r.WantCommp, cause)
	}
	return fmt.Errorf("aggregate %s: %d of %d pieces match an unaggregated row: %s", a.Root, matched, len(a.Pieces), strings.Join(causes, "; "))
}

// loadAggregate rebuilds the aggregate root from its pipeline rows,
// collapsing repeated occurrences of the aggregate into one.
func loadAggregate(ctx context.Context, db *harmonydb.DB, root cid.Cid) (aggtypes.Aggregate, error) {
	var rows []struct {
		Commp          string `db:"commp"`
		AggPosition    *int   `db:"agg_position"`
		InclusionProof []byte `db:"inclusion_proof"`
	}
	if err := db.Select(ctx, &rows, `
		SELECT DISTINCT commp, agg_position, inclusion_proof FROM pdp_blob_pipeline
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
		if i > 0 && r.AggPosition != nil && *r.AggPosition == i-1 {
			return aggtypes.Aggregate{}, fmt.Errorf("aggregate %s: pieces %s and %s are both stored at position %d", root, rows[i-1].Commp, r.Commp, i-1)
		}
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
