package aatodo_types

// Buffer tracks in progress work building an aggregation
type Buffer struct {
	TotalSize           uint64      `cborgen:"totalSize" dagjsongen:"totalSize"`
	ReverseSortedPieces []PieceLink `cborgen:"reverseSortedPieces" dagjsongen:"reverseSortedPieces"`
}
