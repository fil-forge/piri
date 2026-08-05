# Aggregator

Piece aggregation configuration.

| Key | Default | Env | Dynamic |
|-----|---------|-----|---------|
| `pdp.aggregation.aggregator.min_aggregate_size` | `134217728` (128 MiB) | `PIRI_PDP_AGGREGATION_AGGREGATOR_MIN_AGGREGATE_SIZE` | Yes |
| `pdp.aggregation.aggregator.job_queue.workers` | `runtime.NumCPU()` | `PIRI_PDP_AGGREGATION_AGGREGATOR_JOB_QUEUE_WORKERS` | No |
| `pdp.aggregation.aggregator.job_queue.retries` | `50` | `PIRI_PDP_AGGREGATION_AGGREGATOR_JOB_QUEUE_RETRIES` | No |
| `pdp.aggregation.aggregator.job_queue.retry_delay` | `10s` | `PIRI_PDP_AGGREGATION_AGGREGATOR_JOB_QUEUE_RETRY_DELAY` | No |

## Overview

The aggregator groups pieces so that one on-chain `addRoots` transaction covers many blobs.
When a piece's CommP hash is calculated, it enters this queue to be combined with other pieces into an aggregate.

**How aggregation works:**

- Pieces are buffered until their total padded size reaches `min_aggregate_size`, which flushes them as a single aggregate
- A piece larger than `min_aggregate_size` is submitted immediately as a single-piece aggregate
- Pieces still below the threshold stay buffered until more arrive

**Performance Note:** Aggregate creation involves building merkle trees from piece commitments (32-byte hashes).
Memory usage is minimal since only the CommP hashes are held in memory, not the actual blob data.

## Fields

### `min_aggregate_size`

The padded size at which buffered pieces are folded into an aggregate and submitted, in bytes.
Must be a power of two.

This is a **cost-versus-latency** knob:

| Setting | Effect |
|---------|--------|
| Higher | More pieces per `addRoots` transaction, so lower gas per blob — but each blob waits longer before it becomes provable |
| Lower | Blobs become provable sooner, at higher gas per blob |

### `job_queue.workers`

Number of concurrent aggregation operations. Defaults to the number of CPU cores.

- **Higher values**: Faster aggregate creation when many pieces complete CommP calculation simultaneously
- **Lower values**: Reduced concurrency, but memory impact is minimal since only piece hashes are processed

### `job_queue.retries`

Maximum retry attempts before a piece is moved to the dead-letter queue.

### `job_queue.retry_delay`

Wait time between retry attempts after a failure.

## Relationship to maximum piece size

`min_aggregate_size` and [`pdp.piece.max_padded_size`](../piece.md) are independent, and neither is derived from the other.

A note on terms: the on-chain PDP contract only knows the top-level piece it challenges — the aggregate.
"Sub-piece" is the proving pipeline's off-chain bookkeeping for the individual blobs folded into it: when a challenge lands, the prove task looks up which sub-piece contains the challenged offset and builds its merkle tree over that sub-piece alone, assembling the rest of the aggregate root from stored commitments.

The two knobs bound different things:

- **`max_padded_size`** is a memory-safety bound.
  Proving builds a merkle tree over the challenged *sub-piece*, so a single piece's size determines how much memory a proof costs.
- **`min_aggregate_size`** is a gas-versus-latency tradeoff.
  The aggregate root is assembled from sub-piece commitments alone and is never itself turned into a tree, so an aggregate's size costs nothing at proving time.

Aggregate size is instead bounded on-chain by the verifier's `MAX_PIECE_SIZE_LOG2`, which Piri reads from the contract and checks at submission.

Setting `min_aggregate_size` below `max_padded_size` is valid: it just means large pieces bypass batching and go out on their own.

## Dynamic configuration

Changes take effect on the next fold, with no restart:

```console
$ piri client admin config set pdp.aggregation.aggregator.min_aggregate_size 268435456 --persist
```

## TOML

```toml
[pdp.aggregation.aggregator]
min_aggregate_size = 134217728

[pdp.aggregation.aggregator.job_queue]
workers = 2
retries = 50
retry_delay = "10s"
```
