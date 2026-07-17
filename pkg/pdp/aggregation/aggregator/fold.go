package aggregator

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/ipfs/go-cid"

	libpiece "github.com/fil-forge/libforge/piece"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/types"
)

// MinAggregateSize is 128MB
// Max size is 256MB -- this means we will never see an individual piece larger
// than 256MB -- the upload will fail otherwise
// So we can safely assume that if we see a 256MB piece, we just submit immediately
// If not, we can safely aggregate till >=128MB without going over 256MB
const MinAggregateSize = 128 << 20

// AggregatePiece appends newPiece to buffer; when the running size reaches the
// minimum threshold it produces an aggregate and resets the buffer.
//
// The in-memory aggregation logic operates on piri_piece.Piece (which carries
// PaddedSize() and other methods); only the Buffer's serialized form uses
// cid.Cid. The function converts at the boundary.
func AggregatePiece(buffer types.Buffer, newPiece libpiece.Piece) (types.Buffer, *types.Aggregate, error) {
	log.Infow("aggregating piece",
		"link", newPiece.CID().String(),
		"padded size", newPiece.PaddedSize(),
		"buffer size", buffer.TotalSize,
	)
	// if the piece is aggregatable on its own it should submit immediately
	if newPiece.PaddedSize() > MinAggregateSize {
		aggregate, err := NewAggregate([]libpiece.Piece{newPiece})
		if err == nil {
			log.Infow("aggregate create", "root", aggregate.Root)
		}
		return buffer, &aggregate, err
	}

	bufferPieces, err := decodePieces(buffer.ReverseSortedPieces)
	if err != nil {
		return buffer, nil, fmt.Errorf("decoding buffered pieces: %w", err)
	}
	newSize := buffer.TotalSize + newPiece.PaddedSize()
	newPieces := InsertOrderedByDescendingSize(bufferPieces, newPiece)

	// if we have reached the minimum aggregate size, submit and start over
	if newSize >= MinAggregateSize {
		aggregate, err := NewAggregate(newPieces)
		if err != nil {
			return buffer, nil, err
		}
		log.Infow("aggregate create", "root", aggregate.Root)
		return types.Buffer{}, &aggregate, err
	}

	// otherwise keep aggregating
	return types.Buffer{
		TotalSize:           newSize,
		ReverseSortedPieces: encodePieces(newPieces),
	}, nil, nil
}

func AggregatePieces(buffer types.Buffer, pieces []libpiece.Piece) (types.Buffer, []types.Aggregate, error) {
	var aggregates []types.Aggregate
	for _, p := range pieces {
		var aggregate *types.Aggregate
		var err error
		buffer, aggregate, err = AggregatePiece(buffer, p)
		if err != nil {
			return buffer, aggregates, err
		}
		if aggregate != nil {
			aggregates = append(aggregates, *aggregate)
		}
	}
	return buffer, aggregates, nil
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

// decodePieces rehydrates a buffer's persisted CID slice into the
// [libpiece.Piece] form aggregation operates on internally.
func decodePieces(cids []cid.Cid) ([]libpiece.Piece, error) {
	if len(cids) == 0 {
		return nil, nil
	}
	out := make([]libpiece.Piece, len(cids))
	for i, c := range cids {
		p, err := libpiece.FromCID(c)
		if err != nil {
			return nil, fmt.Errorf("decoding piece %s: %w", c, err)
		}
		out[i] = p
	}
	return out, nil
}

func encodePieces(pieces []libpiece.Piece) []cid.Cid {
	if len(pieces) == 0 {
		return nil
	}
	out := make([]cid.Cid, len(pieces))
	for i, p := range pieces {
		out[i] = p.CID()
	}
	return out
}
