# Telemetry

Piri uses [OpenTelemetry](https://opentelemetry.io/) to emit metrics and traces for observability. You can configure custom collectors to send this data to your own monitoring infrastructure.

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
separate scheduler inside Piri, reported by the PDP metrics below.

### PDP Metrics

The chain head the PDP pipeline last saw, the proving schedule and recorded
proving failures of the node's proof sets, and when the proving tasks last
ran. They come from Curio's database, not the chain, so they do not confirm
that a proof landed. See
[Monitoring > PDP Proving Health](../operator-guide/monitoring.md#pdp-proving-health)
for example alerts.

| Metric                                                       | Type  | Unit  | Description                                                  |
|--------------------------------------------------------------|-------|-------|--------------------------------------------------------------|
| <nobr>`piri_chain_head_epoch`</nobr>                         | Gauge | epoch | Epoch of the last tipset the chain scheduler applied         |
| <nobr>`piri_chain_head_timestamp_seconds`</nobr>             | Gauge | s     | Unix timestamp of that tipset                                |
| <nobr>`piri_pdp_proofset_next_challenge_epoch`</nobr>        | Gauge | epoch | Challenge epoch Curio scheduled for the current proving period (written when the scheduling transaction is sent; mid-window for the first period) |
| <nobr>`piri_pdp_proofset_challenge_window_epochs`</nobr>     | Gauge | epoch | Length of the proof set's challenge window                   |
| <nobr>`piri_pdp_proofset_proving_period_epochs`</nobr>       | Gauge | epoch | Length of the proof set's proving period                     |
| <nobr>`piri_pdp_proofset_consecutive_prove_failures`</nobr>  | Gauge | count | Proving transactions Curio has handled as contract reverts since the last successful prove send, which alone resets it |
| <nobr>`piri_pdp_proofset_next_prove_attempt_epoch`</nobr>    | Gauge | epoch | Backoff deadline Curio set after the last proving failure; Curio holds back only while it is ahead of the chain head, and clears it on the next successful prove send |
| <nobr>`piri_pdp_proofsets_unrecoverable`</nobr>              | Gauge | count | Number of proof sets Curio has stopped proving after an unrecoverable failure |
| <nobr>`piri_pdp_task_last_success_timestamp_seconds`</nobr>  | Gauge | s     | Unix time a PDP task last finished a run on the node without a retryable error; for `PDPv0_Prove` this is not "last proof landed" |

**Labels:**

| Label | Description |
|-------|-------------|
| `proof_set` | Proof set ID (the `piri_pdp_proofset_*` metrics; unrecoverable proof sets are only counted in `piri_pdp_proofsets_unrecoverable`) |
| `task_name` | `PDPv0_Prove`, `PDPv0_ProvPeriod` or `PDPv0_InitPP` (`piri_pdp_task_last_success_timestamp_seconds`) |

### IPNI Publishing Metrics

The advertisement queue's backlog. Publishing is asynchronous, so the
`blob/accept` receipt can no longer report a failure to advertise; a backlog
that keeps growing, or an age that does, is how a publisher that is down or
failing shows up.

| Metric                                             | Type  | Unit | Description                                  |
|----------------------------------------------------|-------|------|----------------------------------------------|
| <nobr>`ipni_pending_adverts`</nobr>                | Gauge |      | Advertisements queued and not yet published  |
| <nobr>`ipni_pending_adverts_oldest_seconds`</nobr> | Gauge | s    | Age of the oldest advertisement still queued |

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
| `server_type` | Server mode; `serve full` is the only server, so always `full` |
| `did` | Server's Decentralized Identifier |
| `owner_address` | Ethereum address of node owner |
| `public_url` | Server's publicly accessible URL |
| `proof_set` | PDP proof set ID |

## Traces

Distributed tracing provides end-to-end visibility into operations:

| Span                           | Description                                     |
|--------------------------------|-------------------------------------------------|
| <nobr>`access.grant`</nobr>    | Issuing a delegation in response to `access/grant` |
| <nobr>`blob.allocate`</nobr>   | Allocating space for a blob                     |
| <nobr>`blob.accept`</nobr>     | Accepting an uploaded blob                      |
| <nobr>`blob.reject`</nobr>     | Rejecting an allocation                         |
| <nobr>`blob.release`</nobr>    | Releasing a previously accepted blob            |
| <nobr>`AddRoots`</nobr>        | Adding roots to a PDP proof set                 |

HTTP requests are also traced by the `otelecho` middleware, which names its spans after the
matched route, so those appear alongside the operation spans above. Health checks (`/healthz`,
`/livez` and `/readyz`) are left out of both these traces and the HTTP server metrics: container
healthchecks poll them every few seconds.

Piri traces every request. A request that arrives with a W3C Trace Context `traceparent` header
joins the caller's trace and follows its sampling decision; one that arrives without one starts a
new trace, sampled. This is the OpenTelemetry SDK's default sampler, `parentbased_always_on`. To
sample less, set the standard `OTEL_TRACES_SAMPLER` and `OTEL_TRACES_SAMPLER_ARG` environment
variables, for example `OTEL_TRACES_SAMPLER=parentbased_traceidratio` and
`OTEL_TRACES_SAMPLER_ARG=0.1` to start a trace for one request in ten.

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

Connect your Prometheus datasource and create dashboards using the metrics above. Key metrics to monitor:

- **System health**: `system_cpu_utilization`, `system_memory_used_bytes`, `piri_datadir_free_bytes`
- **Job queue health**: `active_jobs`, `failed_jobs`, `job_duration`
- **API performance**: `http.server.request.duration` (p95, p99)

A collector converting to Prometheus renames these: dots become underscores, a
counter gains `_total`, a unit the name does not already carry is appended, and
a unit-less gauge gains `_ratio`. So the four above are queried as
`system_cpu_utilization_ratio`, `failed_jobs_total`, `job_duration_seconds` and
`http_server_request_duration_seconds`.

Piri reports itself as `service.name` `piri` in `service.namespace` `forge`, the
namespace Forge's services share. A Prometheus-facing collector joins the two
into `job`, as `forge/piri`, and maps the node DID Piri reports as
`service.instance.id` to `instance`. Piri's other resource attributes, such as
its version and deployment environment, are on one `target_info` series per
Piri, joined on `job` and `instance`, rather than on every series; a version
label on every series would start a new series for each metric at every
upgrade. To read the version alongside a metric:

```promql
failed_jobs_total * on (job, instance) group_left (service_version) target_info
```

## Configuration

See [Configuration > telemetry](../configuration/telemetry.md) for collector setup options.
