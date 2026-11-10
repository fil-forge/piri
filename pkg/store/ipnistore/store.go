// Package ipnistore keeps the node's IPNI advertisement chain in Postgres and
// the blob store. The signed head and the per-context maps are rows, so they
// commit in the transaction that publishes a batch; the adverts are objects
// keyed by CID. Small entries chunks are inlined into identity CIDs and not stored.
package ipnistore

import (
	"github.com/fil-forge/go-ipni-tools/pkg/metadata"
	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/fil-forge/piri/pkg/store/objectstore"
)

// MaxIdentityEntries is the most multihashes an advert's entries may hold to
// be inlined into an identity CID instead of stored as a chunk.
const MaxIdentityEntries = 10

// Store is the node's IPNI advertisement chain. It is go-ipni-tools' publisher
// store, and the batch store that publishes a batch with one read of the
// contexts and one transaction for the contexts and the head.
type Store struct {
	store.FullStore
	db       *harmonydb.DB
	blocks   *blockStore
	provider peer.ID
}

// New returns the store for the node with the given peer ID, with its head
// and maps in db and its adverts in objects. Every SQL statement joins the
// harmonytx transaction its context carries.
func New(db *harmonydb.DB, objects objectstore.Store, provider peer.ID) *Store {
	blocks := &blockStore{db: db, objects: objects}
	return &Store{
		FullStore: store.NewPublisherStore(
			blocks,
			&contextTable{db: db, provider: provider, column: chunkLinkColumn},
			&contextTable{db: db, provider: provider, column: metadataColumn},
			store.WithMetadataContext(metadata.MetadataContext),
			store.WithIdentityEntries(MaxIdentityEntries),
		),
		db:       db,
		blocks:   blocks,
		provider: provider,
	}
}
