package aatodo_types

// Blob carries the multihash digest of a stored blob awaiting commP
// computation. It is the payload of the commp job queue.
type Blob struct {
	Digest []byte `cborgen:"digest" dagjsongen:"digest"`
}
