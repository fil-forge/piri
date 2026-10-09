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
| `piri_pdp_proofset_next_challenge_epoch` | The challenge epoch Curio scheduled for each proof set |
| `piri_pdp_proofset_consecutive_prove_failures` | Proving failures Curio has recorded for each proof set since its last successful prove send |
| `piri_pdp_proofsets_unrecoverable` | Proof sets Curio has stopped proving |
| `piri_pdp_task_last_success_timestamp_seconds` | When each PDP task last finished a run without a retryable error |

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
or whose proving is failing, from a healthy one. Piri reports the chain head
and what Curio has recorded in its database, and leaves the judgement to the
alert. None of it is read back from the chain, so none of it confirms that a
proof landed.

| Metric | Unit | Labels | What It Tells You |
|--------|------|--------|-------------------|
| `piri_chain_head_epoch` | epoch | | Epoch of the last tipset the PDP chain scheduler applied |
| `piri_chain_head_timestamp_seconds` | unix seconds | | Timestamp of that tipset (its minimum block timestamp) |
| `piri_pdp_proofset_next_challenge_epoch` | epoch | `proof_set` | The challenge epoch Curio scheduled for the proof set's current proving period (`prove_at_epoch`). Curio writes it when it sends the transaction that schedules the period, so it is Curio's schedule rather than confirmed chain state. For the first period it is the middle of the challenge window, not its start |
| `piri_pdp_proofset_challenge_window_epochs` | epochs | `proof_set` | Length of the proof set's challenge window |
| `piri_pdp_proofset_proving_period_epochs` | epochs | `proof_set` | Length of the proof set's proving period |
| `piri_pdp_proofset_consecutive_prove_failures` | count | `proof_set` | Proving transactions (prove, or scheduling a proving period) that Curio has handled as contract reverts since the proof set's last successful prove send. Only a successful prove send resets it, so after any revert it stays above zero until the next proving period's proof is sent, or indefinitely if proving is then disabled for the set. After each failure Curio backs off before its next proving send, and gives up on the proof set after repeated failures |
| `piri_pdp_proofset_next_prove_attempt_epoch` | epoch | `proof_set` | The backoff deadline Curio set after the proof set's last proving failure. Curio holds back only while it is ahead of the chain head; the value stays after the deadline passes and is cleared on the next successful prove send |
| `piri_pdp_proofsets_unrecoverable` | count | | Number of the node's proof sets Curio has stopped proving after an unrecoverable failure |
| `piri_pdp_task_last_success_timestamp_seconds` | unix seconds | `task_name` | When a run of `PDPv0_Prove`, `PDPv0_ProvPeriod` or `PDPv0_InitPP` last finished on this node without a retryable error. For `PDPv0_Prove` that includes runs that woke too late, found proving disabled, or had their proof rejected: it shows the task is running, not that proofs are landing |

The names above are what Prometheus sees when the OTLP metrics pass through a
collector such as Grafana Alloy: gauges keep their names, and a name that
already ends in `_seconds` gets no extra unit suffix.

The chain head gauges appear once the first tipset arrives after start-up.
The proof set gauges are read from the database at each collection:

- The schedule gauges (`next_challenge_epoch`, `challenge_window_epochs`,
  `proving_period_epochs`) are reported only while a proof set has a scheduled
  challenge epoch. Curio keeps one from the first proving period on; it is
  cleared only when proving is disabled for the proof set (for example, it has
  no data), when the proof set is marked unrecoverable, or when it is reset to
  start its proving periods again.
- A proof set marked unrecoverable has no per-proof-set series at all; it is
  counted in `piri_pdp_proofsets_unrecoverable` instead.

`piri_pdp_task_last_success_timestamp_seconds` has no series for a task that
has not yet finished a run on this node.

Example alert expressions. `30` is the network's block time in seconds (Lotus's
`build.BlockDelaySecs`: 30 on mainnet and Calibration, but a devnet may differ),
which converts between epochs and wall-clock time. The `node` label and `job`
value depend on how your collector labels targets.

```promql
# Chain head stale: the head has not advanced for 5 minutes (Lotus stalled or
# unreachable, or Piri's chain subscription stuck). A run of null rounds, or a
# slow chain scheduler handler that runs before the head is recorded, can also
# trip it, so allow a `for:` of a few minutes.
time() - max by (node) (piri_chain_head_timestamp_seconds{job="forge/piri"}) > 300

# Proving period not advanced: Curio's recorded challenge window for a proof
# set has closed and Curio has not scheduled the next proving period, for a
# reason other than a failure backoff. The current epoch is extrapolated from
# the wall clock, so this still fires when the chain head itself is stale. It
# catches proving-period scheduling having stopped: a stuck head, or the task
# engine not running. A proof set in a failure backoff is left out with
# `unless`: Curio holds its scheduling back on purpose, and "Proving failing"
# below has already paged for it. It does NOT catch a missed proof: Curio
# schedules the next period as soon as the window closes, whether or not the
# proof landed. Use a `for:` of a few minutes, since scheduling happens just
# after the window closes.
(
    piri_pdp_proofset_next_challenge_epoch{job="forge/piri"}
  + piri_pdp_proofset_challenge_window_epochs{job="forge/piri"}
)
< on (node) group_left()
(
    max by (node) (piri_chain_head_epoch{job="forge/piri"})
  + (time() - max by (node) (piri_chain_head_timestamp_seconds{job="forge/piri"})) / 30
)
unless on (node, proof_set)
(
  piri_pdp_proofset_next_prove_attempt_epoch{job="forge/piri"}
  > on (node) group_left()
  (
      max by (node) (piri_chain_head_epoch{job="forge/piri"})
    + (time() - max by (node) (piri_chain_head_timestamp_seconds{job="forge/piri"})) / 30
  )
)

# Proving failing: Curio has had a proving transaction for a proof set rejected
# within the last hour. A rejected prove is not retried within its period, so
# that period's proof was missed; a rejected scheduling transaction is retried
# after a backoff. It fires for about an hour per new failure, so during a long
# backoff it pages after each failure rather than throughout. `increase`
# rather than `delta`, because the count behaves as a counter whose only drop
# is the reset to zero: a reset followed by a new failure within the hour still
# counts as a rise. The count itself is not a
# good paging condition: only a successful prove send resets it, so it stays
# above zero for up to a proving period after any revert, even when the retry
# that follows succeeds, and indefinitely if proving is then disabled for the
# set. Graph `piri_pdp_proofset_consecutive_prove_failures` for the current
# state.
increase(piri_pdp_proofset_consecutive_prove_failures{job="forge/piri"}[1h]) > 0

# Proof set unrecoverable: Curio has given up proving a proof set. Piri does not
# run Curio's data set deletion, so the count does not go down on its own; to
# alert only when a new one appears, use
# `delta(piri_pdp_proofsets_unrecoverable[1h]) > 0`.
piri_pdp_proofsets_unrecoverable{job="forge/piri"} > 0

# Prove task not running: no PDPv0_Prove run has finished in 1.5 proving
# periods. This shows the task engine has stopped proving altogether; it says
# nothing about whether proofs that were sent landed.
(
  time() - max by (node) (piri_pdp_task_last_success_timestamp_seconds{job="forge/piri", task_name="PDPv0_Prove"})
)
> on (node)
(
  1.5 * 30 * max by (node) (max_over_time(piri_pdp_proofset_proving_period_epochs{job="forge/piri"}[1d]))
)
```

What these alerts do not catch: a proof transaction that Curio sent but that
then failed on chain, and a prove task that woke after its challenge window
had closed (Curio only logs this). Both are recorded as successful task runs
and leave the failure count unchanged. The last alert is also per node: with
several proof sets, proving one keeps it quiet.

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
| Proving period not advanced, outside a failure backoff (`piri_pdp_proofset_next_challenge_epoch`) | Critical | Check the chain head and that PDP tasks are running |
| New proving failure for a proof set (`piri_pdp_proofset_consecutive_prove_failures` rising) | Critical | Check logs for the rejected proving transaction |
| Proof set unrecoverable (`piri_pdp_proofsets_unrecoverable`) | Critical | Check logs for why Curio gave up on the proof set |
| Disk space <10% free | Warning | Expand storage or clean up |
| Disk space <5% free | Critical | Immediate action required |
| Failed jobs accumulating | Warning | Check logs for root cause |
| No `PDPv0_Prove` run finished in 1.5 proving periods (`piri_pdp_task_last_success_timestamp_seconds`) | Critical | Verify node is running and healthy |

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
