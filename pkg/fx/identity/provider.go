package identity

import (
	"github.com/fil-forge/ucantone/principal"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
)

var Module = fx.Module("identity",
	fx.Provide(ProvideIdentity),
)

// ProvideIdentity extracts the principal signer from the identity config.
func ProvideIdentity(cfg app.IdentityConfig) principal.Signer {
	return cfg.Signer
}
