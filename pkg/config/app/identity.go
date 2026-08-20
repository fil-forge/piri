package app

import "github.com/fil-forge/ucantone/multikey"

// IdentityConfig contains identity-related configuration
type IdentityConfig struct {
	// The principal issuer for this service
	Issuer multikey.Issuer
}
