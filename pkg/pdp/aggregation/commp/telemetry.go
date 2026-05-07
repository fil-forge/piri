package commp

import (
	"go.opentelemetry.io/otel"
)

var (
	tracer = otel.Tracer("github.com/fil-forge/piri/pkg/pdp/aggregation/commp")
)
