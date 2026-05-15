package pdpfake

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/pdp"
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
		),
		fx.Annotate(
			NewCommp,
			fx.As(fx.Self()),
			fx.As(new(commp.Calculator)),
		),
		fx.Annotate(
			newFakePDP,
			fx.As(new(pdp.PDP)),
		),
	),
)

// fakePDP is a minimal pdp.PDP wrapper. The production wiring goes through
// TODO_PDP_Impl in pkg/fx/pdp, which depends on types.API (a wider interface
// than handlers actually need); satisfying types.API with a fake would mean
// stubbing ProofSetAPI and ProviderAPI for no test benefit.
type fakePDP struct {
	pieces types.PieceAPI
	commp  commp.Calculator
}

func (p *fakePDP) API() types.PieceAPI            { return p.pieces }
func (p *fakePDP) CommpCalculate() commp.Calculator { return p.commp }

func newFakePDP(pieces types.PieceAPI, c commp.Calculator) *fakePDP {
	return &fakePDP{pieces: pieces, commp: c}
}

var _ pdp.PDP = (*fakePDP)(nil)
