package config

import (
	"fmt"
	"os"

	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/piri/pkg/config/app"
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
	id, err := identity.DecodeEd25519SignerFromPEM(pem)
	if err != nil {
		return app.IdentityConfig{}, fmt.Errorf("decoding identity key file: %w", err)
	}
	return app.IdentityConfig{
		Signer: id,
	}, nil
}
