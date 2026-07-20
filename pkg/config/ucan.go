package config

import (
	"fmt"
	"net/url"

	"github.com/fil-forge/piri/pkg/config/app"
)

type UCANServerConfig struct {
	Identity    IdentityConfig    `mapstructure:"identity"`
	Repo        RepoConfig        `mapstructure:"repo"`
	Server      ServerConfig      `mapstructure:"server"`
	UCANService UCANServiceConfig `mapstructure:"ucan"`
}

func (u UCANServerConfig) Validate() error {
	return validateConfig(u)
}

// Normalize applies compatibility fixes before validation.
func (u *UCANServerConfig) Normalize() {
	u.UCANService.Normalize()
}

type UCANServiceConfig struct {
	Services   ServicesConfig `mapstructure:"services" toml:"services"`
	ProofSetID uint64         `mapstructure:"proof_set" flag:"proof-set" toml:"proof_set"`
	// InsecureDIDResolution enables HTTP (instead of HTTPS) for did:web resolution.
	// NB: this should only be used for development purposes.
	InsecureDIDResolution bool `mapstructure:"insecure_did_resolution" toml:"insecure_did_resolution,omitempty"`
	// PLCDirectory is the did:plc directory endpoint used to resolve did:plc
	// DIDs. An omitted or empty value falls back to DefaultPLCDirectory.
	PLCDirectory string `mapstructure:"plc_directory" validate:"omitempty,url" toml:"plc_directory,omitempty"`
}

func (s UCANServiceConfig) Validate() error {
	return validateConfig(s)
}

// Normalize applies compatibility fixes before validation.
func (s *UCANServiceConfig) Normalize() {
	s.Services.Normalize()
}

func (s UCANServiceConfig) ToAppConfig(publicURL url.URL) (app.UCANServiceConfig, error) {
	svcCfg, err := s.Services.ToAppConfig(publicURL)
	if err != nil {
		return app.UCANServiceConfig{}, err
	}
	plcDir, err := parsePLCDirectory(s.PLCDirectory)
	if err != nil {
		return app.UCANServiceConfig{}, err
	}
	return app.UCANServiceConfig{
		Services:              svcCfg,
		ProofSetID:            s.ProofSetID,
		InsecureDIDResolution: s.InsecureDIDResolution,
		PLCDirectory:          plcDir,
	}, nil
}

// parsePLCDirectory converts the configured did:plc directory endpoint into a
// URL. An empty string falls back to DefaultPLCDirectory, so did:plc resolution
// is always available.
func parsePLCDirectory(s string) (*url.URL, error) {
	if s == "" {
		s = DefaultPLCDirectory
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("invalid PLC directory URL %q: %w", s, err)
	}
	return u, nil
}
