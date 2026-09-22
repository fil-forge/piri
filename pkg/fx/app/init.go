package app

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/fx/root"
	"github.com/fil-forge/piri/pkg/health"
)

// InitModule composes the modules `piri register` runs the node with to set
// it up on chain: the PDP half alone, with no UCAN services. It is shared by
// the register command and by the test that validates its wiring, so a
// provider that needs both halves cannot end up in the PDP module unnoticed.
func InitModule(cfg app.AppConfig) fx.Option {
	return fx.Options(
		// Supply init mode for health checks
		fx.Supply(health.ModeInit),
		CommonModules(cfg),
		PDPModule,
		root.Module,
	)
}
