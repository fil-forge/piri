package access

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/ucanhandlers"
)

// Module wires the access/grant capability into the body-CAR RPC server.
var Module = fx.Module("ucan/access",
	fx.Provide(
		ucanhandlers.ProvideRPC(NewGrantHandler),
	),
)
