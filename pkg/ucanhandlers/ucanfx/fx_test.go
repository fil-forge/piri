package ucanfx

import (
	"net/url"
	"testing"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/ucantone/did/resolver"
	"github.com/stretchr/testify/require"
)

func TestNewDIDResolver(t *testing.T) {
	t.Run("without PLC directory supports key and web only", func(t *testing.T) {
		r, err := newDIDResolver(app.UCANServiceConfig{})
		require.NoError(t, err)

		bm, ok := r.(resolver.ByMethod)
		require.True(t, ok, "expected resolver.ByMethod, got %T", r)
		require.Contains(t, bm, "key")
		require.Contains(t, bm, "web")
		require.NotContains(t, bm, "plc")
	})

	t.Run("with PLC directory adds the plc method", func(t *testing.T) {
		plcDir, err := url.Parse("https://plc.directory")
		require.NoError(t, err)

		r, err := newDIDResolver(app.UCANServiceConfig{PLCDirectory: *plcDir})
		require.NoError(t, err)

		bm, ok := r.(resolver.ByMethod)
		require.True(t, ok, "expected resolver.ByMethod, got %T", r)
		require.Contains(t, bm, "key")
		require.Contains(t, bm, "web")
		require.Contains(t, bm, "plc")
	})
}
