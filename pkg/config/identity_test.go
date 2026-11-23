package config

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/ucantone/multikey/ed25519"
	"github.com/stretchr/testify/require"
)

func TestDIDFromPublicURL(t *testing.T) {
	tests := []struct {
		url      string
		insecure bool
		want     string
		wantErr  string
	}{
		{url: "https://piri-0.eu-central-3.staging.example.com", want: "did:web:piri-0.eu-central-3.staging.example.com"},
		{url: "https://node.example.com/", want: "did:web:node.example.com"},
		{url: "https://Node.Example.com", want: "did:web:node.example.com"},
		{url: "https://node.example.com:443", want: "did:web:node.example.com"},
		{url: "https://node.example.com:8443", want: "did:web:node.example.com%3A8443"},
		{url: "http://piri-0:3000", insecure: true, want: "did:web:piri-0%3A3000"},
		{url: "http://piri-0:80", insecure: true, want: "did:web:piri-0"},
		{url: "http://127.0.0.1:3000", insecure: true, wantErr: "not an IP address"},
		{url: "https://node.example.com", insecure: true, want: "did:web:node.example.com"},
		{url: "http://node.example.com", wantErr: "must be an https URL"},
		{url: "http://piri-0:3000", wantErr: "must be an https URL"},
		{url: "https://node.example.com/piri", wantErr: "no path"},
		{url: "https://node.example.com/?x=1", wantErr: "query"},
		{url: "https://node.example.com/#x", wantErr: "fragment"},
		{url: "https://user@node.example.com", wantErr: "user info"},
		{url: "ftp://node.example.com", wantErr: "must be an https URL"},
		{url: "node.example.com", wantErr: "must be an https URL"},
		{url: "ftp://node.example.com", insecure: true, wantErr: "http or https"},
		{url: "http://[::1]:3000", insecure: true, wantErr: "not an IP address"},
		{url: "https://", wantErr: "no host"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			u, err := url.Parse(tt.url)
			require.NoError(t, err)

			got, err := DIDFromPublicURL(*u, tt.insecure)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.String())
		})
	}
}

func TestIdentityConfigToAppConfig(t *testing.T) {
	key, err := ed25519.Generate()
	require.NoError(t, err)
	pem, err := identity.EncodeSignerToPEM(key)
	require.NoError(t, err)
	keyFile := filepath.Join(t.TempDir(), "service.pem")
	require.NoError(t, os.WriteFile(keyFile, pem, 0o600))
	cfg := IdentityConfig{KeyFile: keyFile}

	t.Run("the node is the did:web of its public URL, signing with its key", func(t *testing.T) {
		u, err := url.Parse("https://node.example.com")
		require.NoError(t, err)

		got, err := cfg.ToAppConfig(*u, false)
		require.NoError(t, err)
		require.Equal(t, "did:web:node.example.com", got.Issuer.DID().String())
		require.Equal(t, key.KeyDID(), got.Issuer.KeyDID())
	})

	t.Run("an http public URL is refused unless DIDs are resolved over HTTP", func(t *testing.T) {
		u, err := url.Parse("http://piri-0:3000")
		require.NoError(t, err)

		_, err = cfg.ToAppConfig(*u, false)
		require.ErrorContains(t, err, "must be an https URL")

		got, err := cfg.ToAppConfig(*u, true)
		require.NoError(t, err)
		require.Equal(t, "did:web:piri-0%3A3000", got.Issuer.DID().String())
	})

	t.Run("a public URL with a path is refused", func(t *testing.T) {
		u, err := url.Parse("https://node.example.com/piri")
		require.NoError(t, err)

		_, err = cfg.ToAppConfig(*u, false)
		require.ErrorContains(t, err, "no path")
	})
}

func TestServerConfigRequiresAPublicURL(t *testing.T) {
	_, err := ServerConfig{Host: "localhost", Port: 3000}.ToAppConfig()
	require.ErrorContains(t, err, "public URL is not set")
}
