package blob

import (
	"go.opentelemetry.io/otel"
)

var (
	tracer = otel.Tracer("github.com/fil-forge/piri/pkg/ucanhandlers/blob")
)
