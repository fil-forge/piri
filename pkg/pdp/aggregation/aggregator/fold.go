package aggregator

import (
	"cmp"
	"slices"

	libpiece "github.com/fil-forge/libforge/piece"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/types"
)

// MinAggregateSize is 128MB
// Max size is 256MB -- this means we will never see an individual piece larger
// than 256MB -- the upload will fail otherwise
// So we can safely assume that if we see a 256MB piece, we just submit immediately
// If not, we can safely aggregate till >=128MB without going over 256MB
const MinAggregateSize = 128 << 20

// Append folds pieces, in order, into aggregates. A piece whose padded size
// alone exceeds MinAggregateSize becomes a single-piece aggregate
// immediately; smaller pieces accumulate (kept sorted largest-first, as
// NewAggregate requires) until the running padded size reaches
// MinAggregateSize, which flushes the accumulated set as one aggregate.
// Pieces still below the threshold when the input is exhausted appear in no
// returned aggregate: callers treat absence as still-unaggregated and feed
// those pieces into a later Append.
func Append(pieces []libpiece.Piece) ([]types.Aggregate, error) {
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
		if p.PaddedSize() > MinAggregateSize {
			if err := flush([]libpiece.Piece{p}); err != nil {
				return nil, err
			}
			continue
		}
		buffered = InsertOrderedByDescendingSize(buffered, p)
		total += p.PaddedSize()
		if total >= MinAggregateSize {
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
