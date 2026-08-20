package pdp

import (
	"go.uber.org/fx"

	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/ucanhandlers"
)

// Module wires the pdp/info capability into the body-CAR RPC server,
// plus the adapter from the broad pdp PieceAPI to the narrow PieceResolver
// interface this handler declares.
var Module = fx.Module("ucan/pdp",
	fx.Provide(
		ucanhandlers.ProvideRPC(NewPDPInfoHandler),

		func(p pdptypes.PieceAPI) PieceResolver { return p },
	),
)
