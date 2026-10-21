# CommP

CommP (Piece Commitment) configuration.

| Key | Default | Env | Dynamic |
|-----|---------|-----|---------|
| `pdp.aggregation.commp.delay` | `1h` | `PIRI_PDP_AGGREGATION_COMMP_DELAY` | Yes |
| `pdp.aggregation.commp.job_queue.workers` | `runtime.NumCPU()` | `PIRI_PDP_AGGREGATION_COMMP_JOB_QUEUE_WORKERS` | No |
| `pdp.aggregation.commp.job_queue.retries` | `50` | `PIRI_PDP_AGGREGATION_COMMP_JOB_QUEUE_RETRIES` | No |
| `pdp.aggregation.commp.job_queue.retry_delay` | `10s` | `PIRI_PDP_AGGREGATION_COMMP_JOB_QUEUE_RETRY_DELAY` | No |

## Overview

CommP calculation is the first stage of the aggregation pipeline. When a blob is uploaded, it enters this queue to have 
its Piece Commitment hash calculated. This hash is required for creating aggregates that can be submitted to the 
blockchain.

**Performance Note:** CommP calculation is CPU-intensive. Each worker consumes significant CPU resources while processing a blob.

## Fields

### `delay`

How long an accepted blob waits before its CommP is calculated and it enters aggregation. The blob is
retrievable throughout; only its first proof moves later by this much.

A blob deleted inside the delay costs nothing: no CommP, no parked piece, and no root added on-chain
that its removal would then retire. Deletions of freshly written data cluster in the first minutes
after writing and tail off within about an hour, so the default of one hour saves nearly all the
work a delay can save. Lengthening it past that buys little; shortening it to `0` makes every blob
enter CommP at acceptance.

The setting is read when a blob is claimed, so a change applies to blobs already waiting. Setting it
to `0` releases them on the task engine's next poll (a few seconds).

Alongside the default, `piri_pdp_pipeline_cancelled_age_seconds` (see
[Telemetry](../../../concepts/telemetry.md#aggregation-pipeline-metrics)) records how old each
cancelled blob was, which is the node's own measure of what a given delay would have saved.

### `job_queue.workers`

Number of concurrent CommP calculations. Defaults to the number of CPU cores.

- **Higher values**: Faster throughput when many blobs arrive simultaneously, but higher CPU usage
- **Lower values**: Reduced CPU load, but blobs queue longer during high ingest periods

### `job_queue.retries`

Maximum retry attempts before a blob is moved to the dead-letter queue.

### `job_queue.retry_delay`

Wait time between retry attempts after a failure.

## TOML

```toml
[pdp.aggregation.commp]
delay = "1h"       # Hold accepted blobs before CommP; "0s" disables

[pdp.aggregation.commp.job_queue]
workers = 4        # Limit to 4 cores for shared environments
retries = 50
retry_delay = "10s"
```
