package config

import (
	"fmt"
	"net/url"
	"os"

	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/did/web"
	"github.com/fil-forge/ucantone/multikey"
)

type IdentityConfig struct {
	KeyFile string `mapstructure:"key_file" validate:"required" flag:"key-file" toml:"key_file"`
}

func (i IdentityConfig) Validate() error {
	return validateConfig(i)
}

// ToAppConfig builds the node's identity: the key in the key file, under the
// did:web of the node's public URL. The DID is derived and never configured, so
// it cannot disagree with where the node serves its DID document, and the key
// can be replaced without the node becoming a different node.
//
// insecureDIDResolution says the node runs where did:web documents are fetched
// over HTTP, which is the only place an http public URL can identify it.
func (i IdentityConfig) ToAppConfig(publicURL url.URL, insecureDIDResolution bool) (app.IdentityConfig, error) {
	pem, err := os.ReadFile(i.KeyFile)
	if err != nil {
		return app.IdentityConfig{}, fmt.Errorf("reading identity key file: %w", err)
	}
	key, err := identity.DecodeSignerFromPEM(pem)
	if err != nil {
		return app.IdentityConfig{}, fmt.Errorf("decoding identity key file: %w", err)
	}
	id, err := DIDFromPublicURL(publicURL, insecureDIDResolution)
	if err != nil {
		return app.IdentityConfig{}, err
	}
	return app.IdentityConfig{
		Issuer: multikey.NewIssuer(id, key),
	}, nil
}

// DIDFromPublicURL returns the did:web a node at publicURL is identified by.
//
// A did:web with no path resolves to /.well-known/did.json on its host, which
// is where the node serves its DID document, so the URL has to be the root of
// a host: a path, query, fragment or user info is refused. The scheme is not
// part of the DID. A port is, unless it is the scheme's default.
//
// A did:web document is fetched over HTTPS, so a node at an http URL cannot be
// resolved and the URL is refused. With insecureDIDResolution, for development
// stacks whose services fetch DID documents over HTTP, it is accepted.
func DIDFromPublicURL(publicURL url.URL, insecureDIDResolution bool) (did.DID, error) {
	if publicURL.Scheme == "http" && !insecureDIDResolution {
		return did.Undef, fmt.Errorf("public URL %q must be an https URL: the node's DID document is fetched over HTTPS (ucan.insecure_did_resolution allows http for development)", publicURL.String())
	}
	newDID := web.New
	if insecureDIDResolution {
		newDID = web.NewInsecure
	}
	id, err := newDID(publicURL)
	if err != nil {
		return did.Undef, fmt.Errorf("deriving the node's DID from its public URL: %w", err)
	}
	// web.New turns a path into part of the DID, whose document would then be
	// expected somewhere the node does not serve it.
	if publicURL.Path != "" && publicURL.Path != "/" {
		return did.Undef, fmt.Errorf("public URL %q must have no path: the node's identity is its host, and its DID document is served at /.well-known/did.json", publicURL.String())
	}
	return id, nil
}
