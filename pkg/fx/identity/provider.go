package identity

import (
	"github.com/fil-forge/go-ucanto/principal"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
)

var Module = fx.Module("identity",
	fx.Provide(ProvideIdentity),
)

// ProvideIdentity extracts the principal signer from the app config
func ProvideIdentity(cfg app.AppConfig) principal.Signer {
	return cfg.Identity.Signer
}
