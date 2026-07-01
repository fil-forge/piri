package app

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/health"
)

// FullServerModule composes every fx module required to run the full piri
// server. It is the single source of truth for the full-server dependency
// graph, shared by the `piri serve full` command and by tests that validate
// the wiring via fx.ValidateApp.
func FullServerModule(cfg app.AppConfig) fx.Option {
	return fx.Options(
		// Supply server mode for health checks
		fx.Supply(health.ModeFull),

		// common dependencies of the PDP and UCAN modules:
		//   - identity
		//   - http server
		//   - databases & datastores
		CommonModules(cfg),

		// ucan service dependencies:
		//  - http handlers
		//    - ucan specific handlers, blob allocate and accept, replicate, etc.
		//  - blob, claim, publisher, replicator, and storage services
		UCANModule,

		// pdp service dependencies:
		//  - lotus, eth, and contract clients
		//  - piece aggregator
		//  - task and chain scheduler w/ their related tasks
		//  - http handlers
		//    - create proof set, add root, upload piece, etc.
		//  - address wallet
		PDPModule,
	)
}
