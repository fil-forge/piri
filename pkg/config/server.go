package config

import (
	"fmt"
	"net/url"

	"github.com/fil-forge/piri/pkg/config/app"
)

type ServerConfig struct {
	Port uint   `mapstructure:"port" validate:"required,min=1,max=65535" flag:"port" toml:"port"`
	Host string `mapstructure:"host" validate:"required" flag:"host" toml:"host"`
	// PublicURL is where the node is reached from outside. It is required: the
	// node's DID is derived from it.
	PublicURL string `mapstructure:"public_url" validate:"required,url" flag:"public-url" toml:"public_url"`
}

func (s ServerConfig) Validate() error {
	return validateConfig(s)
}

func (s ServerConfig) ToAppConfig() (app.ServerConfig, error) {
	if s.PublicURL == "" {
		return app.ServerConfig{}, fmt.Errorf("public URL is not set: the node's identity is derived from it")
	}
	publicURL, err := url.Parse(s.PublicURL)
	if err != nil {
		return app.ServerConfig{}, fmt.Errorf("parsing public URL: %w", err)
	}

	return app.ServerConfig{
		Host:      s.Host,
		Port:      s.Port,
		PublicURL: *publicURL,
	}, nil
}
