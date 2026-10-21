// Package ipnistore keeps the node's IPNI advertisement chain in Postgres and
// the blob store. The signed head and the per-context maps are rows, so they
// commit in the transaction that publishes a batch; the adverts are objects
// keyed by CID. Entries chunks are inlined into identity CIDs and not stored.
package ipnistore

import (
	"github.com/fil-forge/go-ipni-tools/pkg/metadata"
	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/fil-forge/piri/pkg/store/objectstore"
)

// New returns the publisher store for the node with the given peer ID, with
// its head and maps in db and its adverts in objects. Every SQL statement
// joins the harmonytx transaction its context carries.
func New(db *harmonydb.DB, objects objectstore.Store, provider peer.ID) store.FullStore {
	return store.NewPublisherStore(
		&blockStore{db: db, objects: objects},
		&contextTable{db: db, provider: provider, column: chunkLinkColumn},
		&contextTable{db: db, provider: provider, column: metadataColumn},
		store.WithMetadataContext(metadata.MetadataContext),
		store.WithIdentityEntries(),
	)
}
