package storage

import (
	"github.com/fil-forge/go-ucanto/client"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	blobhandler "github.com/fil-forge/piri/pkg/service/storage/handlers/blob"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
)

// Module wires the storage service. It does not produce a fat "service"
// object — instead it provides the upload connection and the consumer-side
// interface bindings that the per-handler Deps structs depend on.
var Module = fx.Module("storage",
	fx.Provide(
		NewUploadConnection,
		// Bind concrete production types to the narrow consumer interfaces
		// each handler's Deps struct declares. Each binding is a pass-through
		// that lets fx resolve a handler's narrow dep from its broader
		// concrete provider — keeps the handler-local interfaces honest about
		// what they consume without forcing every store/PDP provider to know
		// about handler-local types.
		func(a allocationstore.AllocationStore) blobhandler.AllocationStore { return a },
		func(a acceptancestore.AcceptanceStore) blobhandler.AcceptanceStore { return a },
		func(p pdptypes.PieceAPI) blobhandler.PieceAllocator { return p },
		func(p pdptypes.PieceAPI) blobhandler.PieceReader { return p },
	),
)

// NewUploadConnection exposes the upload service connection as a top-level
// fx bean so that handlers can request it directly via their Deps structs.
func NewUploadConnection(cfg app.AppConfig) client.Connection {
	return cfg.UCANService.Services.Upload.Connection
}
