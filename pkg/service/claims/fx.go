package claims

import (
	"go.uber.org/fx"

	echofx "github.com/fil-forge/piri/pkg/fx/echo"
)

var Module = fx.Module("claims",
	fx.Provide(
		fx.Annotate(
			NewServer,
			fx.As(new(echofx.RouteRegistrar)),
			fx.ResultTags(`group:"route_registrar"`),
		),
	),
)
