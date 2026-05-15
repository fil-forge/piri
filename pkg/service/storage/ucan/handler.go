package ucan

import (
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/validator"
)

// Handler bundles a UCAN capability with its execution handler so the two can
// be registered together on a [server.HTTPServer]. Mirrors the sprue pattern
// (fil-forge/sprue/pkg/service/handlers).
type Handler struct {
	Capability validator.Capability
	Handler    execution.HandlerFunc
}
