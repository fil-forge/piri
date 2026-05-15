package claims

import (
	"go.uber.org/fx"

	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/service/claims"
)

var Module = fx.Module("claims",
	fx.Provide(
		fx.Annotate(
			claims.NewServer,
			fx.As(new(echofx.RouteRegistrar)),
			fx.ResultTags(`group:"route_registrar"`),
		),
	),
)
