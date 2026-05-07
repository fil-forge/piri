package admin

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/admin/httpapi/handlers"
	echofx "github.com/fil-forge/piri/pkg/fx/echo"
)

var Module = fx.Module("admin",
	fx.Provide(
		fx.Annotate(
			handlers.NewRoutes,
			fx.As(new(echofx.RouteRegistrar)),
			fx.ResultTags(`group:"route_registrar"`),
		),
	),
)
