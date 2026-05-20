package types

import (
	"github.com/ipfs/go-cid"
)

// AggregatorJob is the payload of the aggregator queue — a single
// piece CID handed to the aggregator worker. Wraps the external
// piece.PieceLink interface as a plain cid.Cid so cborgen can
// generate a marshaler; the worker converts back via piece.FromLink.
type AggregatorJob struct {
	Piece cid.Cid `cborgen:"piece"`
}

// ManagerJob is the payload of the manager queue — the list of
// aggregate root CIDs to be submitted to the chain.
type ManagerJob struct {
	Roots []cid.Cid `cborgen:"roots"`
}

// CommpJob is the payload of the commp queue — the multihash digest
// of a blob whose CommP is to be computed. multihash.Multihash is
// `[]byte` under the hood, so we transport it as raw bytes.
type CommpJob struct {
	Digest []byte `cborgen:"digest"`
}
