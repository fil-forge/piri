package types

import (
	"github.com/ipfs/go-cid"
)

// Buffer holds in-progress aggregation state: the running total padded
// size of all queued pieces, plus the pieces themselves sorted
// largest-first by padded size. Persisted between aggregator handler
// runs.
//
// ReverseSortedPieces carries the raw piece CIDs; callers convert back
// to piece.PieceLink via piece.FromLink when they need PaddedSize and
// related accessors.
type Buffer struct {
	TotalSize           uint64    `cborgen:"totalSize"`
	ReverseSortedPieces []cid.Cid `cborgen:"reverseSortedPieces"`
}
