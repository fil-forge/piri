package handlers

import (
	ucanserver "github.com/fil-forge/ucantone/server"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/service/storage/ucan"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

// Module collects the UCAN handlers that the storage service exposes. Each
// `New…Handler` constructor returns a [ucan.Handler] (capability + bound
// handler func); the storage server iterates the `ucan_handlers` group and
// registers each on its [server.HTTPServer].
var Module = fx.Module("storage/ucan/handlers",
	fx.Provide(
		fx.Annotate(
			ucan.NewBlobAllocateHandler,
			fx.ResultTags(`group:"ucan_handlers"`),
		),
		fx.Annotate(
			ucan.NewBlobAcceptHandler,
			fx.ResultTags(`group:"ucan_handlers"`),
		),
		fx.Annotate(
			ucan.NewAccessDelegateHandler,
			fx.ResultTags(`group:"ucan_handlers"`),
		),
		fx.Annotate(
			ucan.NewAccessClaimHandler,
			fx.ResultTags(`group:"ucan_handlers"`),
		),
		fx.Annotate(
			ucan.NewReplicaAllocateHandler,
			fx.ResultTags(`group:"ucan_handlers"`),
		),
		fx.Annotate(
			ucan.NewPDPInfoHandler,
			fx.ResultTags(`group:"ucan_handlers"`),
		),
		fx.Annotate(
			ProvidePersisterOption,
			fx.ResultTags(`group:"ucan_options"`),
		),
	),
)

// ProvidePersisterOption builds the storage-server-side ucantone listener
// that persists every inbound invocation and outbound receipt. Wiring it
// here keeps the per-handler code free of bookkeeping concerns; the
// listener fires once per container codec round-trip.
func ProvidePersisterOption(
	invs invocationstore.InvocationStore,
	rcpts receiptstore.ReceiptStore,
) ucanserver.HTTPOption {
	return ucanserver.WithEventListener(&ucan.Persister{
		Invocations: invs,
		Receipts:    rcpts,
	})
}
