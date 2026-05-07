package ucan

import (
	"go.opentelemetry.io/otel"
)

var (
	tracer = otel.Tracer("github.com/fil-forge/piri/pkg/service/retrieval/ucan")
)
