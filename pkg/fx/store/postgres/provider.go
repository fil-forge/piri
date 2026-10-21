package postgres

import (
	"fmt"

	ipnitools "github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/identity"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/lib"
	"github.com/fil-forge/piri/pkg/database/harmonytx"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/consolidationstore"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
	"github.com/fil-forge/piri/pkg/store/ipnistore"
	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

// Module provides the record stores on the node's harmonydb Postgres
// database, whichever backend holds its blobs.
var Module = fx.Module("postgres-store",
	fx.Provide(
		NewAllocationStore,
		NewAcceptanceStore,
		NewClaimStore,
		NewReceiptStore,
		NewConsolidationStore,
		NewTransactor,
		fx.Annotate(
			NewIPNIStore,
			fx.ParamTags(``, `name:"ipni_objectstore"`),
			// provide as FullStore (self)
			fx.As(fx.Self()),
			// provide sub-interfaces of Full Store
			fx.As(new(ipnitools.PublisherStore)),
			fx.As(new(ipnitools.EncodeableStore)),
		),
	),
)

func NewAllocationStore(db *harmonydb.DB) allocationstore.AllocationStore {
	return allocationstore.NewPostgresStore(db)
}

func NewAcceptanceStore(db *harmonydb.DB) acceptancestore.AcceptanceStore {
	return acceptancestore.NewPostgresStore(db)
}

func NewClaimStore(db *harmonydb.DB) invocationstore.InvocationStore {
	return invocationstore.NewPostgresStore(db)
}

func NewReceiptStore(db *harmonydb.DB) receiptstore.ReceiptStore {
	return receiptstore.NewPostgresStore(db)
}

func NewConsolidationStore(db *harmonydb.DB) consolidationstore.Store {
	return consolidationstore.NewPostgresStore(db)
}

// NewIPNIStore keeps the node's IPNI advertisement chain: the head and the
// per-context maps in db, the adverts in objects.
func NewIPNIStore(db *harmonydb.DB, objects objectstore.Store, id identity.Identity) (ipnitools.FullStore, error) {
	key, err := lib.Libp2pKey(id)
	if err != nil {
		return nil, fmt.Errorf("creating IPNI store: %w", err)
	}
	provider, err := peer.IDFromPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("creating IPNI store: deriving peer ID: %w", err)
	}
	return ipnistore.New(db, objects, provider), nil
}

// NewTransactor runs writes that span the record stores as one transaction.
func NewTransactor(db *harmonydb.DB) store.Transactor {
	return harmonytx.New(db)
}
