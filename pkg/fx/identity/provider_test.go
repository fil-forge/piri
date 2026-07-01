package identity

import (
	"testing"

	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/ucantone/multikey"
	"github.com/fil-forge/ucantone/multikey/ed25519"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
)

// The identity module must expose the service identity under every shape the fx
// graph consumes it as: the concrete identity.Identity, ucan.Issuer, and
// ucan.Signer. Consumers such as manager.NewPieceAccepter (ucan.Issuer) and the
// replica/egress handlers (ucan.Signer) fail to build otherwise.
func TestModuleProvidesIdentityShapes(t *testing.T) {
	signer, err := ed25519.Generate()
	require.NoError(t, err)
	cfg := app.IdentityConfig{Issuer: multikey.KeyIssuer(signer)}

	err = fx.ValidateApp(
		fx.Supply(cfg),
		Module,
		fx.Invoke(func(identity.Identity, ucan.Issuer, ucan.Signer) {}),
	)
	require.NoError(t, err, "identity module must provide identity.Identity, ucan.Issuer and ucan.Signer")
}
