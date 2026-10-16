package config

import (
	"fmt"

	"github.com/fil-forge/piri/pkg/config/app"
)

// GeneratedConfigVersion identifies what `piri init` writes for a given set of
// inputs. Bump it whenever a change to init alters the config it generates from
// the same base config and flags: a newly honoured base-config section, a field
// init now sets or derives differently. Leave it alone for changes that do not.
//
// init records it in the generated config as `config_version`, and `piri version
// --config-version` prints it, so a deployment can tell that a config was written by an
// init that predates the running binary and re-run init. TestGeneratedConfig in
// cmd/cli/setup fails when init's output changes without a bump.
const GeneratedConfigVersion = 1

type FullServerConfig struct {
	// ConfigVersion is the GeneratedConfigVersion of the init that wrote this
	// config, or zero for a config written by hand or by an older init.
	ConfigVersion int               `mapstructure:"config_version" toml:"config_version,omitempty"`
	Network       string            `mapstructure:"network" flag:"network" toml:"network,omitempty"`
	Identity      IdentityConfig    `mapstructure:"identity" toml:"identity"`
	Repo          RepoConfig        `mapstructure:"repo" toml:"repo"`
	Server        ServerConfig      `mapstructure:"server" toml:"server"`
	PDPService    PDPServiceConfig  `mapstructure:"pdp" toml:"pdp"`
	UCANService   UCANServiceConfig `mapstructure:"ucan" toml:"ucan"`
	Telemetry     TelemetryConfig   `mapstructure:"telemetry" toml:"telemetry,omitempty"`
}

func (f FullServerConfig) Validate() error {
	return validateConfig(f)
}

// Normalize applies compatibility fixes before validation.
func (f *FullServerConfig) Normalize() {
	f.UCANService.Normalize()
}

func (f FullServerConfig) ToAppConfig() (app.AppConfig, error) {
	var (
		err error
		out app.AppConfig
	)

	//
	// user provided configuration
	//
	out.Identity, err = f.Identity.ToAppConfig()
	if err != nil {
		return app.AppConfig{}, fmt.Errorf("converting identity to app config: %s", err)
	}

	out.Server, err = f.Server.ToAppConfig()
	if err != nil {
		return app.AppConfig{}, fmt.Errorf("converting server config to app config: %s", err)
	}

	out.Storage, err = f.Repo.ToAppConfig()
	if err != nil {
		return app.AppConfig{}, fmt.Errorf("converting repo to app config: %s", err)
	}

	out.UCANService, err = f.UCANService.ToAppConfig(out.Server.PublicURL)
	if err != nil {
		return app.AppConfig{}, fmt.Errorf("converting services to app config: %s", err)
	}

	out.PDPService, err = f.PDPService.ToAppConfig()
	if err != nil {
		return app.AppConfig{}, fmt.Errorf("converting local pdp to app config: %s", err)
	}

	out.Telemetry = f.Telemetry.ToAppConfig()

	//
	// non-user configuration
	//
	out.Replicator = app.DefaultReplicatorConfig()

	return out, nil
}
