# telemetry

Observability configuration for metrics and distributed tracing. Piri exports telemetry only to the
collectors listed here; with none configured, it exports nothing.

A failed export is logged at `WARN` by the `telemetry` logger. While an error repeats, as it does
when a collector is unreachable, Piri logs it on its 1st, 2nd, 4th, 8th and later power-of-two
occurrences, with the count in the `occurrences` field. Each distinct error is counted separately,
and an error that goes five minutes without recurring starts again from one.

| Key                     | Default                         | Env                          | Dynamic |
|-------------------------|---------------------------------|------------------------------|---------|
| `telemetry.environment` | `network`, or `custom` if unset | `PIRI_TELEMETRY_ENVIRONMENT` | No      |

## Fields

### `environment`

The deployment environment telemetry is reported under. It becomes the
`deployment.environment.name` resource attribute on every metric and span.

Defaults to the configured `network`, which is meaningful for a node running against a
[network preset](../concepts/networks.md). A node configured from a base config usually has no
network, and should name its own environment instead; otherwise it reports `custom`.

### `metrics`

Array of OTLP metrics collector configurations. Each entry supports:

| Field              | Required | Description                                                    |
|--------------------|----------|----------------------------------------------------------------|
| `endpoint`         | Yes      | OTLP/HTTP collector as `host:port`, with no scheme and no path |
| `insecure`         | No       | Use HTTP instead of HTTPS (default: `false`)                   |
| `publish_interval` | No       | How often to publish metrics (Go duration, default: `30s`)     |
| `headers`          | No       | Custom HTTP headers (e.g., for authentication)                 |

### `traces`

Array of OTLP trace collector configurations. Each entry supports:

| Field      | Required | Description                                                    |
|------------|----------|----------------------------------------------------------------|
| `endpoint` | Yes      | OTLP/HTTP collector as `host:port`, with no scheme and no path |
| `insecure` | No       | Use HTTP instead of HTTPS (default: `false`)                   |
| `headers`  | No       | Custom HTTP headers                                            |

### About `endpoint`

Piri exports over OTLP/HTTP, whose conventional port is **4318** (4317 is the gRPC port, which Piri
does not use). The endpoint is a host and optional port only: Piri appends `/v1/metrics` or
`/v1/traces` itself. Writing a scheme (`https://collector:4318`) or a path makes the exporter treat
the whole string as a hostname, and every publish then fails to resolve.

A collector that serves OTLP on a path other than the default — Grafana Cloud's OTLP gateway, for
instance, which expects `/otlp/v1/metrics` — cannot be addressed directly. Point Piri at a collector
you run and let that collector forward.

See [Concepts > Telemetry](../concepts/telemetry.md) for details on available metrics and traces.

## TOML

```toml
[telemetry]
environment = "staging"

[[telemetry.metrics]]
endpoint = "collector:4318"
insecure = true
publish_interval = "30s"

[[telemetry.traces]]
endpoint = "otel.example.com:4318"
headers = { Authorization = "Bearer ..." }
```
