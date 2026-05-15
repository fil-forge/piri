package handlers

import (
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/service/retrieval/ucan"
)

// Module collects the UCAN handlers exposed by the retrieval server. Each
// `New…Handler` constructor returns a [ucan.Handler]; the retrieval server
// iterates the `retrieval_ucan_handlers` group and registers each on its
// [server.HTTPServer]. Mirrors the storage-side pattern.
var Module = fx.Module("retrieval/ucan/handlers",
	fx.Provide(
		fx.Annotate(
			ucan.NewContentRetrieveHandler,
			fx.ResultTags(`group:"retrieval_ucan_handlers"`),
		),
		fx.Annotate(
			ucan.NewBlobRetrieveHandler,
			fx.ResultTags(`group:"retrieval_ucan_handlers"`),
		),
	),
)
