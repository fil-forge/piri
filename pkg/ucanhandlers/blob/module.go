package blob

import (
	"go.uber.org/fx"

	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/ucanhandlers"
)

// Module wires the blob/* capabilities. fx.As re-exposes the broad
// concrete types (allocationstore, acceptancestore, pdp PieceAPI) as the
// narrow interfaces each handler declares.
var Module = fx.Module("ucan/blob",
	fx.Provide(
		ucanhandlers.ProvideRPC(NewAcceptHandler),
		ucanhandlers.ProvideRPC(NewBlobAllocateHandler),
		ucanhandlers.ProvideRPC(NewBlobReleaseHandler),
		ucanhandlers.ProvideRPC(NewBlobRejectHandler),
		ucanhandlers.ProvideRetrieval(NewBlobRetrieveHandler),

		fx.Annotate(
			func(a allocationstore.AllocationStore) allocationstore.AllocationStore { return a },
			fx.As(new(AllocationStore)),
			fx.As(new(AllocationRemover)),
		),
		fx.Annotate(
			func(a acceptancestore.AcceptanceStore) acceptancestore.AcceptanceStore { return a },
			fx.As(new(AcceptanceStore)),
			fx.As(new(AcceptanceRemover)),
			fx.As(new(AcceptanceChecker)),
		),
		fx.Annotate(
			func(p pdptypes.PieceRemoverAPI) pdptypes.PieceRemoverAPI { return p },
			fx.As(new(PieceRemover)),
		),
		fx.Annotate(
			func(p pdptypes.PieceAPI) pdptypes.PieceAPI { return p },
			fx.As(new(PieceAllocator)),
			fx.As(new(PieceReader)),
		),
	),
)
