package app

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/health"
	"github.com/fil-forge/piri/pkg/service/publisher"
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

		// the IPNI advertisement queue and the task that drains it: the
		// task publishes through the UCAN module's publisher service on the
		// PDP module's harmonydb, so it belongs to neither half and only to
		// their composition. `piri register` runs the PDP half alone.
		publisher.QueueModule,
	)
}
