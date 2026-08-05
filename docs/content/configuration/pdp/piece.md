# Piece

Bounds the size of a single piece (blob) this node will accept.

| Key | Default | Env | Dynamic |
|-----|---------|-----|---------|
| `pdp.piece.max_padded_size` | `268435456` (256 MiB) | `PIRI_PDP_PIECE_MAX_PADDED_SIZE` | Yes |

## Overview

This is the limit enforced when a blob is allocated and again when it is uploaded.
An allocation above it fails with a `BlobSizeLimitExceeded` receipt; an upload above it is cut off mid-stream with `413 Request Entity Too Large`.

The value is a **padded** size — the size of the FR32 merkle tree the piece occupies, not the number of bytes an uploader sends.
The raw limit is derived from it and is always smaller.

## Why padded, and why a power of two

A padded tree size is always a power of two, so raw sizes round *up* to the next one.
That makes a raw limit a trap: the difference between two adjacent raw values can double the memory a proof costs.

| Raw bytes | Pads to | Memtree peak |
|-----------|---------|--------------|
| `266338304` | 256 MiB | ~768 MiB |
| `266338305` | **512 MiB** | **~1.5 GiB** |

Configuring the padded size makes that impossible to get wrong.
Non-powers-of-two are rejected rather than silently rounded, because a limit of 384 MiB would otherwise behave exactly as 256 MiB with no indication why.

To convert: `max_raw = max_padded / 128 * 127`.
So the 256 MiB default admits raw blobs up to `266338304` bytes.

| `max_padded_size` | Raw limit | Memtree peak |
|-------------------|-----------|--------------|
| `268435456` (256 MiB) | `266338304` | ~768 MiB |
| `536870912` (512 MiB) | `532676608` | ~1.5 GiB |
| `1073741824` (1 GiB) | `1065353216` | ~3 GiB |

## The floor

The minimum accepted value is the default itself: **256 MiB** (`268435456`).
The limit is raise-only.
Network clients decide how to shard an upload against a network-wide constant, before they know which node will store it, so a node configured below the network default would reject blobs every other node accepts.
An operator may accept more than the default, never less.

## The ceiling

The maximum accepted value is **1 GiB** (`1073741824`), and it is not adjustable.

Proving builds a full in-memory merkle tree over the challenged sub-piece, and Curio's prove task cannot build one larger than this.
Exceeding it would not fail at ingest — it would fail later, at proving time, as a **registered fault**.

Peak memory during a proof is roughly **3× the padded size**, and Curio budgets 3 GiB for the prove task.
Piri currently has no proof cache, so *every* proof takes this path; there is no cached fast path to fall back on.
Raise this knob only if the node has memory to match.

## Relationship to aggregation

None.
[`min_aggregate_size`](aggregation/aggregator.md) is a separate knob measuring a different thing — see that page.

## Dynamic configuration

Changes take effect immediately, with no restart:

```console
$ piri client admin config set pdp.piece.max_padded_size 536870912 --persist
```

Values that are not a power of two, are below the 256 MiB default, or exceed 1 GiB are rejected.

An upload already in flight when the limit is lowered is re-checked against the new limit and refused.

Lowering the limit bounds **future ingest only**.
Pieces accepted under a higher limit remain stored, keep proving, and keep their ~3× memory cost at proving time for as long as they are in a data set — raising the limit temporarily does not shed the obligation when it is lowered again.

## TOML

```toml
[pdp.piece]
max_padded_size = 268435456
```
