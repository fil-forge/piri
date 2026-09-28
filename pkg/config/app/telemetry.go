package app

import "time"

type TelemetryConfig struct {
	// Environment is the deployment environment telemetry is reported under.
	Environment string
	Metrics     []TelemetryCollectorConfig
	Traces      []TelemetryCollectorConfig
}

type TelemetryCollectorConfig struct {
	Endpoint        string
	Insecure        bool
	Headers         map[string]string
	PublishInterval time.Duration
}
