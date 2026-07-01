package identity

import (
	"go.uber.org/fx"

	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/ucantone/ucan"

	"github.com/fil-forge/piri/pkg/config/app"
)

var Module = fx.Module("identity",
	fx.Provide(
		fx.Annotate(
			ProvideIdentity,
			fx.As(fx.Self()),        // concrete identity.Identity
			fx.As(new(ucan.Issuer)), // consumed by e.g. manager.NewPieceAccepter
			fx.As(new(ucan.Signer)), // consumed by e.g. replica and egress handlers
		),
	),
)

// ProvideIdentity extracts the issuer from the identity config.
func ProvideIdentity(cfg app.IdentityConfig) identity.Identity {
	return identity.Identity{Issuer: cfg.Issuer}
}
