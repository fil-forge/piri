package storage

import (
	"go.uber.org/fx"

	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	blobhandler "github.com/fil-forge/piri/pkg/service/storage/handlers/blob"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
)

// Module wires the storage service. It provides only the consumer-side
// interface bindings that the per-handler Deps structs depend on — each
// binding is a pass-through that lets fx resolve a handler's narrow dep
// from its broader concrete provider.
var Module = fx.Module("storage",
	fx.Provide(
		func(a allocationstore.AllocationStore) blobhandler.AllocationStore { return a },
		func(a acceptancestore.AcceptanceStore) blobhandler.AcceptanceStore { return a },
		func(p pdptypes.PieceAPI) blobhandler.PieceAllocator { return p },
		func(p pdptypes.PieceAPI) blobhandler.PieceReader { return p },
	),
)
