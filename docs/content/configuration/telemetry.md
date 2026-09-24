# telemetry

Observability configuration for metrics and distributed tracing. Piri exports telemetry only to the
collectors listed here; with none configured, it exports nothing.

## Fields

### `metrics`

Array of OTLP metrics collector configurations. Each entry supports:

| Field              | Required | Description                                                |
|--------------------|----------|------------------------------------------------------------|
| `endpoint`         | Yes      | OTLP HTTP endpoint (e.g., `https://otel.example.com:4317`) |
| `insecure`         | No       | Use HTTP instead of HTTPS (default: `false`)               |
| `publish_interval` | No       | How often to publish metrics (Go duration, e.g., `30s`)    |
| `headers`          | No       | Custom HTTP headers (e.g., for authentication)             |

### `traces`

Array of OTLP trace collector configurations. Each entry supports:

| Field      | Required | Description                                  |
|------------|----------|----------------------------------------------|
| `endpoint` | Yes      | OTLP HTTP endpoint                           |
| `insecure` | No       | Use HTTP instead of HTTPS (default: `false`) |
| `headers`  | No       | Custom HTTP headers                          |

See [Concepts > Telemetry](../concepts/telemetry.md) for details on available metrics and traces.

## TOML

```toml
[[telemetry.metrics]]
endpoint = "https://otel.example.com:4317"
insecure = false
publish_interval = "30s"
headers = { Authorization = "Bearer ..." }

[[telemetry.traces]]
endpoint = "https://otel.example.com:4317"
insecure = false
headers = { Authorization = "Bearer ..." }
```
