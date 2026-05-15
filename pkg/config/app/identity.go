package app

import (
	"github.com/fil-forge/ucantone/principal"
)

// IdentityConfig contains identity-related configuration
type IdentityConfig struct {
	// The principal signer for this service
	Signer principal.Signer
}
