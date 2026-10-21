package app

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/fx/store"
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
		ipniAdvertisingModules(cfg),
	)
}

// ipniAdvertisingModules returns what IPNI advertising adds to the server:
// the advertisement chain store, the route serving it and the task
// publishing the queue into it. With no announce URLs configured it returns
// none of them, so nothing is written under the publisher data dir; the
// publisher service then caches claims with the indexer and queues nothing.
func ipniAdvertisingModules(cfg app.AppConfig) fx.Option {
	if !cfg.UCANService.Services.Publisher.IPNIEnabled() {
		return fx.Options()
	}
	return fx.Options(
		store.PublisherStoreModule(cfg.Storage),
		publisher.ServerModule,
		publisher.PublishTaskModule,
	)
}
