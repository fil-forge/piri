package pdpfake

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
	"github.com/fil-forge/piri/pkg/pdp/types"
)

// Module supplies in-memory fakes for the PDP backend. Include it alongside
// app.UCANModule in fxtest applications to route handlers through the PDP
// code path without instantiating the real PDP service.
var Module = fx.Module("pdpfake",
	fx.Provide(
		fx.Annotate(
			NewPieces,
			fx.As(fx.Self()),
			fx.As(new(types.PieceAPI)),
			fx.As(new(types.PieceReaderAPI)),
			fx.As(new(types.PieceRemoverAPI)),
		),
		fx.Annotate(
			NewCommp,
			fx.As(fx.Self()),
			fx.As(new(commp.Calculator)),
		),
	),
)
