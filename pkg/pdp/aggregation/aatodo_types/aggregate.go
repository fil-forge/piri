package aatodo_types

// ProofData is a merkle inclusion proof for a piece within an aggregate. Path
// holds the sibling node hashes from leaf to root; Index is the leaf position.
type ProofData struct {
	Path  [][]byte `cborgen:"path" dagjsongen:"path"`
	Index uint64   `cborgen:"index" dagjsongen:"index"`
}

// AggregatePiece is a single sub-piece of an aggregate together with the
// inclusion proof needed to prove it against the aggregate root.
type AggregatePiece struct {
	Link           PieceLink `cborgen:"link" dagjsongen:"link"`
	InclusionProof ProofData `cborgen:"inclusionProof" dagjsongen:"inclusionProof"`
}

// Aggregate is a piece formed by combining a set of sub-pieces sorted largest
// to smallest, along with the inclusion proof for each sub-piece.
type Aggregate struct {
	Root   PieceLink        `cborgen:"root" dagjsongen:"root"`
	Pieces []AggregatePiece `cborgen:"pieces" dagjsongen:"pieces"`
}
