// Package advert holds what a queued IPNI advertisement is generated from.
package advert

import "github.com/multiformats/go-multihash"

// Spec is what one queued advertisement is generated from. It is computed
// from the location commitment when the claim is queued and stored in the
// queue row as CBOR, so publishing needs nothing but the row and the shape
// can change without a database migration: the generated decoder ignores
// fields it does not know and zeroes fields it does not find.
type Spec struct {
	// ContextID is the IPNI context ID, derived from the space and content.
	ContextID []byte `cborgen:"contextID"`
	// Digest is the content multihash the advertisement lists.
	Digest multihash.Multihash `cborgen:"digest"`
	// Metadata is the marshalled LocationCommitmentMetadata.
	Metadata []byte `cborgen:"metadata"`
}
