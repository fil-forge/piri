package access

import (
	logging "github.com/ipfs/go-log/v2"
	"go.opentelemetry.io/otel"
)

var (
	log    = logging.Logger("ucan/access/grant")
	tracer = otel.Tracer("github.com/fil-forge/piri/pkg/ucanhandlers/access")
)
