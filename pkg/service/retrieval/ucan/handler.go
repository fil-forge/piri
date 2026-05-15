package ucan

import (
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/validator"
)

// Handler bundles a UCAN capability with its execution handler so the two can
// be registered together on a retrieval [server.HTTPServer]. Mirrors the same
// shape used by the storage UCAN handlers (pkg/service/storage/ucan).
type Handler struct {
	Capability validator.Capability
	Handler    execution.HandlerFunc
}
