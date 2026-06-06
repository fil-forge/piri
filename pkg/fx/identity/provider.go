package identity

import (
	"go.uber.org/fx"

	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/piri/pkg/config/app"
)

var Module = fx.Module("identity",
	fx.Provide(ProvideIdentity),
)

// ProvideIdentity extracts the issuer from the identity config.
func ProvideIdentity(cfg app.IdentityConfig) identity.Identity {
	return identity.Identity{Issuer: cfg.Issuer}
}
