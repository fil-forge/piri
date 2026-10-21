package access

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/ucanhandlers"
)

// Module wires the access/grant capability into the body-CAR RPC server. It is
// served without the subject checks: /access/grant is the bootstrap step of the
// access flow, so the issuer has no prior delegation to present and invokes it
// self-signed with this node as the audience (see pkg/service/proofs/caching.go).
var Module = fx.Module("ucan/access",
	fx.Provide(
		fx.Annotate(NewGrantHandler, fx.ResultTags(ucanhandlers.RPCOpenHandlersGroupTag)),
	),
)
