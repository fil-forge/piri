package ucanfx

import (
	"net/url"
	"testing"

	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/did/resolver"
	"github.com/fil-forge/ucantone/multikey"
	"github.com/fil-forge/ucantone/multikey/ed25519"
	"github.com/stretchr/testify/require"
)

func TestNewDIDResolver(t *testing.T) {
	key, err := ed25519.Generate()
	require.NoError(t, err)

	t.Run("adds the plc method", func(t *testing.T) {
		plcDir, err := url.Parse("https://plc.directory")
		require.NoError(t, err)

		r, err := newDIDResolver(app.UCANServiceConfig{PLCDirectory: *plcDir}, identity.Identity{Issuer: multikey.KeyIssuer(key)})
		require.NoError(t, err)

		bm, ok := r.(resolver.ByMethod)
		require.True(t, ok, "expected resolver.ByMethod, got %T", r)
		require.Contains(t, bm, "key")
		require.Contains(t, bm, "web")
		require.Contains(t, bm, "plc")
	})

	t.Run("resolves the node's own did:web without fetching it", func(t *testing.T) {
		// Nothing serves this host, so the document can only come from the
		// node's own identity.
		self, err := did.Parse("did:web:node.invalid")
		require.NoError(t, err)
		id := identity.Identity{Issuer: multikey.NewIssuer(self, key)}

		r, err := newDIDResolver(app.UCANServiceConfig{}, id)
		require.NoError(t, err)

		want, err := id.DIDDocument()
		require.NoError(t, err)
		got, err := r.Resolve(t.Context(), self)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
}
