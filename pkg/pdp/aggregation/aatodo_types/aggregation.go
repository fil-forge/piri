package aatodo_types

import "github.com/ipfs/go-cid"

// Aggregation is a batch of aggregate root CIDs pending submission to the PDP
// service. It serves as both the manager's buffered state and its queue payload.
type Aggregation struct {
	Roots []cid.Cid `cborgen:"roots" dagjsongen:"roots"`
}
