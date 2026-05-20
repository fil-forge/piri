package content

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/ucanhandlers"
)

// Module wires the space/content/retrieve capability into the byte-streaming
// retrieval server.
var Module = fx.Module("ucan/content",
	fx.Provide(
		ucanhandlers.ProvideRetrieval(NewRetrieveHandler),
	),
)
