package proofs

import (
	"go.uber.org/fx"
)

var Module = fx.Module("proofs",
	fx.Provide(
		fx.Annotate(
			NewCachingProofService,
			fx.As(fx.Self()),
			fx.As(new(ProofService)),
		),
	),
)
