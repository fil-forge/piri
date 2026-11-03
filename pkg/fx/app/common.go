package app

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/admin"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/config/dynamic"
	"github.com/fil-forge/piri/pkg/fx/database"
	"github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/fx/identity"
	"github.com/fil-forge/piri/pkg/fx/store"
	"github.com/fil-forge/piri/pkg/health"
	"github.com/fil-forge/piri/pkg/pdp/piecesize"
	piecesizepolicy "github.com/fil-forge/piri/pkg/pdp/piecesize/policy"
	"github.com/fil-forge/piri/pkg/service/proofs"
)

func CommonModules(cfg app.AppConfig) fx.Option {
	var modules = []fx.Option{
		// Supply top level config, and it's sub-configs
		// this allows dependencies to be taken on, for example, app.ServerConfig or app.StorageConfig
		// instead of needing to depend on the top level app.AppConfig
		fx.Supply(cfg),
		fx.Supply(cfg.Identity),
		fx.Supply(cfg.Server),
		fx.Supply(cfg.Storage),
		fx.Supply(cfg.UCANService),
		fx.Supply(cfg.UCANService.Services),
		fx.Supply(cfg.UCANService.Services.Upload),
		fx.Supply(cfg.UCANService.Services.Indexer),
		fx.Supply(cfg.UCANService.Services.Publisher),
		fx.Supply(cfg.UCANService.Services.EgressTracker),
		fx.Supply(cfg.PDPService),
		fx.Supply(cfg.Replicator),
		fx.Supply(cfg.PDPService.SigningService),
		fx.Supply(cfg.PDPService.Piece),
		fx.Supply(cfg.PDPService.Aggregation.Aggregator),
		fx.Supply(cfg.PDPService.Aggregation.Manager),
		fx.Supply(cfg.PDPService.Gas),

		identity.Module, // Provides identity.Identity, ucan.Issuer and ucan.Signer
		proofs.Module,   // Provides service for requesting service proofs
		echo.Module,     // Provides Echo server with route registration
		database.Module, // Provides SQLite database for job queues
		dynamic.Module,  // Provides dynamic configuration registry

		// Provides piecesize.Policy, the piece-size limit read through the
		// dynamic registry. Lives in CommonModules rather than PDPModule
		// because the blob/allocate handler in UCANModule needs it, and the
		// ucanfxtest suites compose CommonModules + UCANModule with no
		// PDPModule.
		fx.Provide(piecesizepolicy.New),
		// fx constructors are lazy, and the constructor is what registers
		// pdp.piece.max_padded_size with the config registry. Force it so the
		// key is present in GET /admin/config regardless of which consumers
		// happen to be wired in — otherwise dropping the last consumer would
		// silently remove the key from the admin surface.
		fx.Invoke(func(piecesize.Policy) {}),

		admin.Module,  // Provides admin module with http routes.
		health.Module, // Provides health check endpoints.

		// StorageModule returns the appropriate storage module based on configuration.
		// If S3 is configured, returns S3Module + KeyStoreModule (KeyStore always on disk).
		// Otherwise, returns the full filesystem module.
		store.StorageModule(cfg.Storage),
	}

	return fx.Module("common", modules...)

}
