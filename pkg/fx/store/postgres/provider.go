package postgres

import (
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/consolidationstore"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
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

// NewTransactor runs writes that span the record stores as one transaction.
func NewTransactor(db *harmonydb.DB) store.Transactor {
	return harmonytx.New(db)
}
