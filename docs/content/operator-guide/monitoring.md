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
| `piri_pdp_commp_held_blobs` | Accepted blobs waiting out the CommP delay |
| `piri_pdp_commp_due_blobs` | Blobs past the delay that no CommP task has picked up (should stay near zero) |
| `piri_pdp_commp_due_blobs_oldest_seconds` | How long the oldest of those has waited |
| `piri_pdp_pipeline_cancelled` | Blobs removed before aggregation, by whether their CommP had been computed |
| `system_cpu_utilization` | CPU usage |
| `system_memory_used_bytes` | Memory usage |
| `piri_datadir_free_bytes` | Available disk space |
| `piri_chain_head_timestamp_seconds` | When the chain head the PDP pipeline last saw was produced |
| `piri_pdp_proofset_next_challenge_epoch` | The challenge epoch Curio scheduled for each proof set |
| `piri_pdp_proofsets_unrecoverable` | Proof sets Curio has stopped proving |
| `piri_pdp_task_last_success_timestamp_seconds` | When each PDP task last finished a run without a retryable error |
| `piri_pdp_db_pool_acquires_waited` | PDP database acquires that waited for a free connection (rising share means the pool is too small) |
| `piri_pdp_db_pool_acquire_wait_seconds` | Time those acquires spent waiting |

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
# set has closed and Curio has not scheduled the next proving period. The
# current epoch is extrapolated from the wall clock, so this still fires when
# the chain head itself is stale. It catches proving-period scheduling having
# stopped: a stuck head, or the task engine not running. It does NOT catch a
# missed proof: Curio schedules the next period as soon as the window closes,
# whether or not the proof landed. Use a `for:` of a few minutes, since
# scheduling happens just after the window closes.
(
    piri_pdp_proofset_next_challenge_epoch{job="forge/piri"}
  + piri_pdp_proofset_challenge_window_epochs{job="forge/piri"}
)
< on (node) group_left()
(
    max by (node) (piri_chain_head_epoch{job="forge/piri"})
  + (time() - max by (node) (piri_chain_head_timestamp_seconds{job="forge/piri"})) / 30
)

# Proof set unrecoverable: Curio has given up proving a proof set. Piri does not
# run Curio's data set deletion, so the count does not go down on its own; to
# alert only when a new one appears, use
# `increase(piri_pdp_proofsets_unrecoverable{job="forge/piri"}[1h]) > 0`.
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

### Aggregation Pipeline Health

An accepted blob waits out
[`pdp.aggregation.commp.delay`](../configuration/pdp/aggregation/commp.md#delay)
before a CommP task claims it. `piri_pdp_commp_held_blobs` is that queue
working as configured: it tracks the ingest rate times the delay. Once a blob
is past the delay the task engine claims it within a poll, so
`piri_pdp_commp_due_blobs` sits near zero and
`piri_pdp_commp_due_blobs_oldest_seconds` near the poll interval. A due count
that keeps growing, or an oldest age of minutes, means claims have stopped:
the task engine is down, or every poll finds other work first.

| Metric | Unit | Labels | What It Tells You |
|--------|------|--------|-------------------|
| `piri_pdp_commp_held_blobs` | count | | Accepted blobs still inside the CommP delay |
| `piri_pdp_commp_due_blobs` | count | | Accepted blobs past the delay with no CommP task yet |
| `piri_pdp_commp_due_blobs_oldest_seconds` | seconds | | How long the oldest of those has been past the delay |
| `piri_pdp_pipeline_cancelled` | count | `stage` | Blobs released before aggregation. `before_commp` ones cost no CommP and no on-chain root; `after_commp` ones had their CommP computed and piece parked, and finalization cleans that up |
| `piri_pdp_pipeline_cancelled_age_seconds` | seconds | `stage` | Age of each cancelled blob since acceptance. The share of `before_commp` cancellations under the delay is what the delay is saving; the histogram's shape says whether a shorter or longer delay would save more |

```promql
# CommP claims stalled: a blob has been past the delay for 10 minutes without a
# CommP task claiming it. The engine claims due blobs on each poll (seconds),
# so this is the task engine down or saturated, not a slow CommP.
max by (node) (piri_pdp_commp_due_blobs_oldest_seconds{job="forge/piri"}) > 600
```

### PDP Database Pool

Every PDP task and every `blob/accept` shares one Postgres connection pool,
sized by the `pool_max_conns` parameter on the Postgres URL (see
[Database configuration](../configuration/repo/database.md)). Each accept makes
several short queries on it, and a burst of accepts and the PDP tasks slow each
other down when the pool is too small. The pool counters say whether that is
happening: the share of acquires that waited for a connection, and how long
they waited on average. Raise `pool_max_conns` while either is high, within
what Postgres's `max_connections` leaves for the other services on the host.

| Metric | Unit | Labels | What It Tells You |
|--------|------|--------|-------------------|
| `piri_pdp_db_pool_connections` | count | `state` | Open connections held by a caller (`acquired`) or waiting unused (`idle`). Acquired at the maximum for sustained periods is the pool saturated |
| `piri_pdp_db_pool_max_connections` | count | | The configured pool size, for the panel and the ratio |
| `piri_pdp_db_pool_acquires` | count | | Connections handed out |
| `piri_pdp_db_pool_acquire_seconds` | seconds | | Time spent obtaining a connection, waiting or not |
| `piri_pdp_db_pool_acquires_waited` | count | | Acquires that waited because no connection was free |
| `piri_pdp_db_pool_acquire_wait_seconds` | seconds | | Time those acquires spent waiting |
| `piri_pdp_db_pool_acquires_cancelled` | count | | Acquires given up because the request ended first, such as an accept timing out in the queue |

The counters are cumulative, so Prometheus sees them with a `_total` suffix
(`piri_pdp_db_pool_acquires_total`, `piri_pdp_db_pool_acquire_wait_seconds_total`).

```promql
# Share of acquires that waited for a connection over the last 5 minutes.
sum by (node) (rate(piri_pdp_db_pool_acquires_waited_total{job="forge/piri"}[5m]))
/ sum by (node) (rate(piri_pdp_db_pool_acquires_total{job="forge/piri"}[5m]))

# Mean wait of the acquires that waited, in seconds.
sum by (node) (rate(piri_pdp_db_pool_acquire_wait_seconds_total{job="forge/piri"}[5m]))
/ sum by (node) (rate(piri_pdp_db_pool_acquires_waited_total{job="forge/piri"}[5m]))

# Pool saturated: a tenth of acquires waited for a connection over 15 minutes.
# Occasional waits during a burst are fine; a sustained share means blob/accept
# and the PDP tasks are queueing for the pool. Raise pool_max_conns.
sum by (node) (rate(piri_pdp_db_pool_acquires_waited_total{job="forge/piri"}[15m]))
/ sum by (node) (rate(piri_pdp_db_pool_acquires_total{job="forge/piri"}[15m])) > 0.1
```

## Logs

Piri logs operational events. Adjust log levels dynamically:

```bash
# Increase verbosity for a subsystem
piri client admin log set pdp debug

# List all log subsystems
piri client admin log list
```

When troubleshooting, increase verbosity for the relevant subsystem, reproduce the issue, then review logs.

The HTTP server logs under the `fx/echo` subsystem: one entry per request, plus the messages of
the HTTP framework and of Go's HTTP server, such as TLS handshake failures. A request whose
handler panicked is logged at error level as `server error`, with the panic message and the stack
trace in the `error` field. The client receives a 500 response that says only
`Internal Server Error`.

The request log's `remote_ip` field and the HTTP spans' `client.address` attribute hold the
client's address. When the request arrives from a loopback, link-local or private address, such
as the nginx proxy in front of Piri, Piri reads the client address from `X-Forwarded-For`.
Otherwise it uses the address of the connection, so a client connecting directly cannot set its
own. A proxy on a public address is not trusted: its own address is recorded instead.

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
| Proving period not advanced (`piri_pdp_proofset_next_challenge_epoch`) | Critical | Check the chain head and that PDP tasks are running |
| Proof set unrecoverable (`piri_pdp_proofsets_unrecoverable`) | Critical | Check logs for why Curio gave up on the proof set |
| Disk space <10% free | Warning | Expand storage or clean up |
| Disk space <5% free | Critical | Immediate action required |
| Failed jobs accumulating | Warning | Check logs for root cause |
| No `PDPv0_Prove` run finished in 1.5 proving periods (`piri_pdp_task_last_success_timestamp_seconds`) | Critical | Verify node is running and healthy |
| A tenth of PDP database acquires waited for a connection over 15 minutes (`piri_pdp_db_pool_acquires_waited`) | Warning | Raise `pool_max_conns` on the Postgres URL |

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
