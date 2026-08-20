package aggregator

import (
	"cmp"
	"slices"

	libpiece "github.com/fil-forge/libforge/piece"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/types"
)

// Append folds pieces, in order, into aggregates. A piece whose padded size
// alone exceeds the threshold becomes a single-piece aggregate immediately;
// smaller pieces accumulate (kept sorted largest-first, as NewAggregate
// requires) until the running padded size reaches the threshold, which
// flushes the accumulated set as one aggregate. Pieces still below the
// threshold when the input is exhausted appear in no returned aggregate:
// callers treat absence as still-unaggregated and feed those pieces into a
// later Append.
//
// The threshold trades gas against latency — a larger one amortizes the
// on-chain addRoots transaction over more pieces but makes each blob wait
// longer to become provable. It is deliberately unrelated to the maximum
// piece size, which is a memory-safety bound: proving builds a merkle tree
// per challenged *sub-piece*, never over the aggregate, so an aggregate's
// size costs nothing at proving time. Aggregates are bounded only by the
// on-chain MAX_PIECE_SIZE_LOG2, checked in pkg/pdp/service/roots_add.go.
//
// Append does not re-check individual piece sizes. That is enforced at
// ingest, in blob/allocate and AllocatePiece.
func Append(pieces []libpiece.Piece, policy Policy) ([]types.Aggregate, error) {
	minAggregateSize := policy.MinAggregateSize()

	var (
		aggregates []types.Aggregate
		buffered   []libpiece.Piece // sorted largest-first by padded size
		total      uint64
	)
	flush := func(ps []libpiece.Piece) error {
		aggregate, err := NewAggregate(ps)
		if err != nil {
			return err
		}
		log.Infow("aggregate create", "root", aggregate.Root)
		aggregates = append(aggregates, aggregate)
		return nil
	}
	for _, p := range pieces {
		// a piece that is aggregatable on its own submits immediately
		if p.PaddedSize() > minAggregateSize {
			if err := flush([]libpiece.Piece{p}); err != nil {
				return nil, err
			}
			continue
		}
		buffered = InsertOrderedByDescendingSize(buffered, p)
		total += p.PaddedSize()
		if total >= minAggregateSize {
			if err := flush(buffered); err != nil {
				return nil, err
			}
			buffered, total = nil, 0
		}
	}
	return aggregates, nil
}

// InsertOrderedByDescendingSize adds a piece to a list of pieces sorted
// largest to smallest, maintaining sort order.
func InsertOrderedByDescendingSize(sortedPieces []libpiece.Piece, newPiece libpiece.Piece) []libpiece.Piece {
	pos, _ := slices.BinarySearchFunc(sortedPieces, newPiece, func(test, target libpiece.Piece) int {
		// flip ordering comparing size cause we're going in reverse order
		return cmp.Compare(target.PaddedSize(), test.PaddedSize())
	})
	return slices.Insert(sortedPieces, pos, newPiece)
}
