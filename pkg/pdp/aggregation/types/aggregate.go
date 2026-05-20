package types

import (
	"github.com/filecoin-project/go-data-segment/merkletree"
	"github.com/ipfs/go-cid"
	"go.uber.org/zap/zapcore"
)

// AggregatePiece is one sub-piece of an Aggregate. Link is the piece
// CID (CIDv1 with Piece multicodec). InclusionProof is the merkle
// path proving Link sits at a specific position in the aggregate's
// merkle tree.
//
// cborgen tags drive the generated MarshalCBOR/UnmarshalCBOR in
// cbor_gen.go. merkletree.ProofData carries its own MarshalCBOR
// (encoding.go:14-24 in go-data-segment) so cborgen delegates rather
// than re-encoding the field structure.
type AggregatePiece struct {
	Link           cid.Cid              `cborgen:"link"`
	InclusionProof merkletree.ProofData `cborgen:"inclusionProof"`
}

// Aggregate is the root of an aggregation tree plus the sub-pieces
// (with inclusion proofs) it commits to.
type Aggregate struct {
	Root   cid.Cid          `cborgen:"root"`
	Pieces []AggregatePiece `cborgen:"pieces"`
}

// MarshalLogObject makes Aggregate satisfy zapcore.ObjectMarshaler.
func (a Aggregate) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("root", a.Root.String())
	return enc.AddArray("pieces", zapcore.ArrayMarshalerFunc(func(arr zapcore.ArrayEncoder) error {
		for _, p := range a.Pieces {
			arr.AppendString(p.Link.String())
		}
		return nil
	}))
}
