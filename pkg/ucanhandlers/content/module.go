package content

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/ucanhandlers"
)

// Module wires the space/content/retrieve capability into the byte-streaming
// retrieval server. Its subject is the space, not this node, and a space's own
// key holder may retrieve its content directly, so neither subject check
// applies.
var Module = fx.Module("ucan/content",
	fx.Provide(
		fx.Annotate(NewRetrieveHandler, fx.ResultTags(ucanhandlers.RetrievalSpaceHandlersGroupTag)),
	),
)
