package config

import (
	"time"

	"github.com/fil-forge/piri/pkg/config/app"
)

type TelemetryCollectorConfig struct {
	Endpoint        string            `mapstructure:"endpoint" validate:"required" toml:"endpoint"`
	Insecure        bool              `mapstructure:"insecure" toml:"insecure,omitempty"`
	Headers         map[string]string `mapstructure:"headers" toml:"headers,omitempty"`
	PublishInterval time.Duration     `mapstructure:"publish_interval" toml:"publish_interval,omitempty"`
}

type TelemetryConfig struct {
	// Environment names the deployment telemetry is reported under, and
	// becomes the deployment.environment.name resource attribute. It defaults
	// to the configured network, which is only meaningful for a node running
	// against a network preset; a node configured from a base config sets it
	// explicitly.
	Environment string                     `mapstructure:"environment" toml:"environment,omitempty"`
	Metrics     []TelemetryCollectorConfig `mapstructure:"metrics" toml:"metrics,omitempty"`
	Traces      []TelemetryCollectorConfig `mapstructure:"traces" toml:"traces,omitempty"`
}

func (t TelemetryConfig) Validate() error {
	return validateConfig(t)
}

func (t TelemetryConfig) ToAppConfig() app.TelemetryConfig {
	convert := func(in []TelemetryCollectorConfig) []app.TelemetryCollectorConfig {
		out := make([]app.TelemetryCollectorConfig, 0, len(in))
		for _, c := range in {
			out = append(out, app.TelemetryCollectorConfig{
				Endpoint:        c.Endpoint,
				Insecure:        c.Insecure,
				Headers:         c.Headers,
				PublishInterval: c.PublishInterval,
			})
		}
		return out
	}

	return app.TelemetryConfig{
		Environment: t.Environment,
		Metrics:     convert(t.Metrics),
		Traces:      convert(t.Traces),
	}
}
