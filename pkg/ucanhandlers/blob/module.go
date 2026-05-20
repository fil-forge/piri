package blob

import (
	"go.uber.org/fx"

	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/ucanhandlers"
)

// Module wires the blob/* capabilities. Adapter providers pass the broader
// concrete types (allocationstore, acceptancestore, pdp PieceAPI) through
// to the narrow interfaces each handler declares.
var Module = fx.Module("ucan/blob",
	fx.Provide(
		ucanhandlers.ProvideRPC(NewAcceptHandler),
		ucanhandlers.ProvideRPC(NewBlobAllocateHandler),
		ucanhandlers.ProvideRetrieval(NewBlobRetrieveHandler),

		func(a allocationstore.AllocationStore) AllocationStore { return a },
		func(a acceptancestore.AcceptanceStore) AcceptanceStore { return a },
		func(p pdptypes.PieceAPI) PieceAllocator { return p },
		func(p pdptypes.PieceAPI) PieceReader { return p },
	),
)
