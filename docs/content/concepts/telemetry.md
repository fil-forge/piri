# Telemetry

Piri uses [OpenTelemetry](https://opentelemetry.io/) to emit metrics and traces for observability. Configure collectors to send this data to your own monitoring infrastructure; Piri exports nothing until you do.

## Metrics

Piri emits metrics via OTLP (OpenTelemetry Protocol) that can be consumed by any compatible collector.

### Host Metrics

System-level metrics for monitoring node health:

| Metric                                   | Type  | Unit  | Description                         |
|------------------------------------------|-------|-------|-------------------------------------|
| <nobr>`system_cpu_utilization`</nobr>    | Gauge | 0-1   | System-wide CPU utilization         |
| <nobr>`system_memory_used_bytes`</nobr>  | Gauge | bytes | System memory in use                |
| <nobr>`system_memory_total_bytes`</nobr> | Gauge | bytes | Total system memory                 |
| <nobr>`piri_datadir_used_bytes`</nobr>   | Gauge | bytes | Disk space used by data directory   |
| <nobr>`piri_datadir_free_bytes`</nobr>   | Gauge | bytes | Free disk space for data directory  |
| <nobr>`piri_datadir_total_bytes`</nobr>  | Gauge | bytes | Total disk space for data directory |

### Job Queue Metrics

Track task execution in internal job queues:

| Metric                      | Type          | Description                      |
|-----------------------------|---------------|----------------------------------|
| <nobr>`active_jobs`</nobr>  | UpDownCounter | Currently running jobs           |
| <nobr>`queued_jobs`</nobr>  | UpDownCounter | Jobs waiting in queue            |
| <nobr>`failed_jobs`</nobr>  | Counter       | Permanently failed jobs          |
| <nobr>`job_duration`</nobr> | Histogram     | Job execution duration (seconds) |

**Labels:**

| Label | Description |
|-------|-------------|
| `queue` | Name of the job queue: `replication` or `egress-tracker` |
| `job_name` | Type of job being executed |
| `status` | Job outcome (`success` or `failure`) |
| `attempt` | Retry attempt number (1-based) |
| `failure_reason` | Reason for permanent failure (only on `failed_jobs`) |

The job's type is `job_name` and not `job` because a collector turns each
attribute into a Prometheus label, and `job` there is the reserved target label
it derives from the service name. An attribute of that name silently replaces
it.

`replication` and `egress-tracker` are the only two queues. PDP runs on a
separate scheduler inside Piri which emits no metrics of its own.

### HTTP Server Metrics

Standard OpenTelemetry HTTP instrumentation:

| Metric                                        | Type      | Description        |
|-----------------------------------------------|-----------|--------------------|
| <nobr>`http.server.request.duration`</nobr>   | Histogram | Request latency    |
| <nobr>`http.server.request.body.size`</nobr>  | Histogram | Request body size  |
| <nobr>`http.server.response.body.size`</nobr> | Histogram | Response body size |

### Server Info

Build and runtime information:

| Metric                          | Type | Description     |
|---------------------------------|------|-----------------|
| <nobr>`piri_server_info`</nobr> | Info | Server metadata |

**Labels:**

| Label | Description |
|-------|-------------|
| `version` | Piri software version |
| `commit` | Git commit hash of the build |
| `built_by` | Build system identifier |
| `build_date` | When the binary was compiled |
| `start_time_unix` | Server start time (Unix timestamp) |
| `server_type` | Server mode (`full` or `ucan`) |
| `did` | Server's Decentralized Identifier |
| `owner_address` | Ethereum address of node owner |
| `public_url` | Server's publicly accessible URL |
| `proof_set` | PDP proof set ID |

## Traces

Distributed tracing provides end-to-end visibility into operations:

| Span                                  | Description                  |
|---------------------------------------|------------------------------|
| <nobr>`blob.accept`</nobr>            | Blob acceptance operations   |
| <nobr>`blob.allocate`</nobr>          | Blob allocation operations   |
| <nobr>`space.content.retrieve`</nobr> | Content retrieval operations |
| <nobr>`AddRoots`</nobr>               | PDP root addition operations |

Piri samples no trace of its own: the sampler is parent-based with a
never-sample root, so a span is recorded only when an incoming request already
carries a sampled W3C Trace Context. A node that no other service traces into
exports nothing here, however its `[[telemetry.traces]]` is configured.

## Integration

### Prometheus

Use an [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/) with a Prometheus exporter:

```yaml
# otel-collector-config.yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: "0.0.0.0:4318"

exporters:
  prometheus:
    endpoint: "0.0.0.0:9090"

service:
  pipelines:
    metrics:
      receivers: [otlp]
      exporters: [prometheus]
```

Configure Piri to send metrics to your collector:

```toml
[[telemetry.metrics]]
endpoint = "localhost:4318"
insecure = true
publish_interval = "30s"
```

### Jaeger

For distributed tracing, configure a Jaeger backend with OTLP support:

```toml
[[telemetry.traces]]
endpoint = "jaeger:4318"
insecure = true
```

### Grafana

Connect your Prometheus datasource and create dashboards using the metrics
above. Key metrics to monitor:

- **System health**: `system_cpu_utilization`, `system_memory_used_bytes`, `piri_datadir_free_bytes`
- **Job queue health**: `active_jobs`, `failed_jobs`, `job_duration`
- **API performance**: `http.server.request.duration` (p95, p99)

A collector converting to Prometheus renames these: dots become underscores, a
counter gains `_total`, a unit the name does not already carry is appended, and
a unit-less gauge gains `_ratio`. So the four above are queried as
`system_cpu_utilization_ratio`, `failed_jobs_total`, `job_duration_seconds` and
`http_server_request_duration_seconds`.

Piri's resource attributes — its version, its node DID as `service.instance.id`
and its deployment environment — do not become labels on each series. A
Prometheus-facing collector maps the service name to `job`, the instance ID to
`instance`, and carries the rest on a `target_info` series to join against.

## Configuration

See [Configuration > telemetry](../configuration/telemetry.md) for collector setup options.

## What a node reports about itself

Piri sends telemetry only to the collectors you configure. See
[Operations > Telemetry](../operations/telemetry.md) for what a node reports
about itself when you do.
