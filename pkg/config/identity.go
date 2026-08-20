package config

import (
	"fmt"
	"os"

	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/ucantone/multikey"
)

type IdentityConfig struct {
	KeyFile string `mapstructure:"key_file" validate:"required" flag:"key-file" toml:"key_file"`
}

func (i IdentityConfig) Validate() error {
	return validateConfig(i)
}

func (i IdentityConfig) ToAppConfig() (app.IdentityConfig, error) {
	pem, err := os.ReadFile(i.KeyFile)
	if err != nil {
		return app.IdentityConfig{}, fmt.Errorf("reading identity key file: %w", err)
	}
	id, err := identity.DecodeSignerFromPEM(pem)
	if err != nil {
		return app.IdentityConfig{}, fmt.Errorf("decoding identity key file: %w", err)
	}
	return app.IdentityConfig{
		Issuer: multikey.KeyIssuer(id),
	}, nil
}
