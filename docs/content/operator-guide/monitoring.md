# Monitoring Your Node

Day-to-day operation involves watching for issues before they become problems. This page covers what to monitor and how.

## Key Health Indicators

### Lotus Sync Status

Your Lotus node must stay synced. If it falls behind, your Piri node can't track epochs correctly and may miss proof windows.

Check Lotus sync:

```bash
lotus sync status
```

Look for the chain height and sync status. If significantly behind the network height, investigate immediately.

### Proof Set State

Your proof set state tells you whether proving is healthy:

```bash
piri client pdp proofset state
```

Watch for:

- **Challenge issued but not proven**: Your node should generate proofs promptly
- **Fault state**: Indicates a missed challenge window—investigate why
- **Epochs until next challenge**: Shows how much time before the next proof is due

### Job Queue Health

Monitor your job queues for stuck or failed jobs:

| Queue | Purpose |
|-------|---------|
| `replication` | Data replication transfers |
| `egress-tracker` | Retrieval event submission |

These are the only two job queues. Piece aggregation and the rest of PDP run on
a separate scheduler (Curio's harmonytask engine), which the queue metrics do
not cover; see [PDP Proving Health](#pdp-proving-health) for its metrics.

A growing backlog or high failure rate indicates problems. Check logs for error details.

### Disk Space

Monitor free space in your data directory:

```bash
df -h /path/to/data_dir
```

Running out of space will cause failures. Set up alerts well before capacity is reached.

### System Resources

Basic system health:

- **CPU**: Sustained high usage may indicate resource constraints
- **Memory**: Watch for memory pressure or swapping
- **Network**: Ensure bandwidth is sufficient for replication and retrieval

## Telemetry

Piri emits OpenTelemetry metrics and traces for detailed observability.

### Key Metrics

| Metric | What It Tells You |
|--------|-------------------|
| `active_jobs` | Currently running jobs per queue |
| `queued_jobs` | Jobs waiting in queue |
| `failed_jobs` | Permanently failed jobs (investigate these) |
| `job_duration` | How long jobs take |
| `ipni_pending_adverts` | IPNI advertisements queued and not yet published |
| `ipni_pending_adverts_oldest_seconds` | Age of the oldest queued advertisement |
| `system_cpu_utilization` | CPU usage |
| `system_memory_used_bytes` | Memory usage |
| `piri_datadir_free_bytes` | Available disk space |
| `piri_chain_head_timestamp_seconds` | When the chain head the PDP pipeline last saw was produced |
| `piri_pdp_proofset_next_challenge_epoch` | When each proof set's next challenge window opens |
| `piri_pdp_task_last_success_timestamp_seconds` | When each PDP task last succeeded |

### Setting Up Metrics Collection

Configure a metrics endpoint:

```toml
[[telemetry.metrics]]
endpoint = "your-collector:4318"
insecure = true
publish_interval = "30s"
```

`endpoint` is a host and optional port, with no scheme and no path: Piri exports over OTLP/HTTP
(port 4318 by convention) and appends `/v1/metrics` itself. See
[Configuration > telemetry](../configuration/telemetry.md).

Send metrics to Prometheus, Grafana, or any OTLP-compatible backend.

### PDP Proving Health

These gauges let an alert tell a node whose chain view has stopped advancing,
or that has stopped proving, from a healthy one. Piri reports the raw schedule
and progress and leaves the judgement to the alert.

| Metric | Unit | Labels | What It Tells You |
|--------|------|--------|-------------------|
| `piri_chain_head_epoch` | epoch | | Epoch of the last tipset the PDP chain scheduler applied |
| `piri_chain_head_timestamp_seconds` | unix seconds | | Timestamp of that tipset (its minimum block timestamp) |
| `piri_pdp_proofset_next_challenge_epoch` | epoch | `proof_set` | Epoch the proof set's next challenge window opens |
| `piri_pdp_proofset_challenge_window_epochs` | epochs | `proof_set` | Length of the proof set's challenge window |
| `piri_pdp_proofset_proving_period_epochs` | epochs | `proof_set` | Length of the proof set's proving period |
| `piri_pdp_task_last_success_timestamp_seconds` | unix seconds | `task_name` | When `PDPv0_Prove`, `PDPv0_ProvPeriod` or `PDPv0_InitPP` last completed successfully on this node |

The names above are what Prometheus sees when the OTLP metrics pass through a
collector such as Grafana Alloy: gauges keep their names, and a name that
already ends in `_seconds` gets no extra unit suffix.

The chain head gauges appear once the first tipset arrives after start-up.
The proof set gauges are read from the database at each collection, and only
for proof sets that have a next challenge scheduled: between a proof and the
scheduling of the next proving period a proof set has none, so its series is
briefly absent. `piri_pdp_task_last_success_timestamp_seconds` has no series
for a task that has not yet succeeded on this node.

Example alert expressions (Filecoin epochs are 30 seconds; the `node` label
and `job` value depend on how your collector labels targets):

```promql
# The chain head has not advanced for 5 minutes (Lotus stalled or
# unreachable, or Piri's chain subscription stuck).
time() - max by (node) (piri_chain_head_timestamp_seconds{job="forge/piri"}) > 300

# A proof set's challenge window has closed without the next proving period
# being scheduled. The current epoch is extrapolated from the wall clock, so
# this still fires when the chain head itself is stale. Use a `for:` of a few
# minutes: the next proving period is scheduled just after the window closes.
(
    piri_pdp_proofset_next_challenge_epoch{job="forge/piri"}
  + piri_pdp_proofset_challenge_window_epochs{job="forge/piri"}
)
< on (node) group_left
(
    max by (node) (piri_chain_head_epoch{job="forge/piri"})
  + (time() - max by (node) (piri_chain_head_timestamp_seconds{job="forge/piri"})) / 30
)

# No successful proof in 1.5 proving periods.
(
  time() - max by (node) (piri_pdp_task_last_success_timestamp_seconds{job="forge/piri", task_name="PDPv0_Prove"})
)
> on (node)
(
  1.5 * 30 * max by (node) (max_over_time(piri_pdp_proofset_proving_period_epochs{job="forge/piri"}[1d]))
)
```

The last alert is per node: with several proof sets, one proving successfully
keeps it quiet while another does not. The challenge-window alert is the
per-proof-set check.

## Logs

Piri logs operational events. Adjust log levels dynamically:

```bash
# Increase verbosity for a subsystem
piri client admin log set pdp debug

# List all log subsystems
piri client admin log list
```

When troubleshooting, increase verbosity for the relevant subsystem, reproduce the issue, then review logs.

## Health Endpoint

Your node exposes a health endpoint:

```bash
curl https://your-node.example.com/health
```

Use this for load balancer health checks or uptime monitoring.

## Alerts to Configure

Recommended alerts:

| Condition | Severity | Action |
|-----------|----------|--------|
| Lotus sync behind by >100 epochs | Critical | Check Lotus node immediately |
| Chain head not advancing for 5 minutes (`piri_chain_head_timestamp_seconds`) | Critical | Check Lotus and Piri's connection to it |
| Proof set past its challenge window (`piri_pdp_proofset_next_challenge_epoch`) | Critical | Investigate missed proof |
| Disk space <10% free | Warning | Expand storage or clean up |
| Disk space <5% free | Critical | Immediate action required |
| Failed jobs accumulating | Warning | Check logs for root cause |
| No successful proof in 1.5 proving periods (`piri_pdp_task_last_success_timestamp_seconds`) | Critical | Verify node is running and healthy |

## Regular Checks

**Daily:**

- Verify Lotus is synced
- Check proof set state for faults
- Review failed job counts

**Weekly:**

- Review disk space trends
- Check for software updates (`piri status upgrade-check`)
- Verify wallet balance for gas

**Monthly:**

- Review overall job success rates
- Check egress payment status
- Consider settling storage payments if accumulated

## Troubleshooting

### Proofs Not Submitting

1. Check Lotus sync status
2. Verify wallet has FIL for gas
3. Check proof set state for errors
4. Review PDP task logs

### Replication Failing

1. Check the `replication` queue for stuck jobs
2. Verify network connectivity to source
3. Check disk space
4. Review the replicator logs

### High Job Failure Rate

1. Identify which queue is failing
2. Check logs for specific error messages
3. Verify external dependencies (Lotus, network, disk)
4. Check if issues are transient or persistent

For detailed troubleshooting, see the specific subsystem's documentation and logs.
