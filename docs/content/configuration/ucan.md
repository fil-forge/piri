# ucan

UCAN service configuration.

| Key | Default | Env | Dynamic |
|-----|---------|-----|---------|
| `ucan.proof_set` | - | `PIRI_UCAN_PROOF_SET` | No |

## Fields

### `proof_set`

Proof set ID from initialization.

## TOML

```toml
[ucan]
proof_set = 123
```

<details>
<summary>Preset-Managed Fields</summary>

These fields are automatically configured by the `network` preset. You typically don't need to set them manually. See [presets](../presets.md) for details.

## [ucan.services]

External service connections.

### [ucan.services.indexer]

```toml
[ucan.services.indexer]
did = "did:web:indexer.forge.storacha.network"
url = "https://indexer.forge.storacha.network/claims"
proof = "..."  # Optional delegation proof
```

The integration is optional. Leaving `did` or `url` empty disables it: location
claims are then not cached with an indexer. The indexer setting has no effect
on IPNI advertising, which `ipni_announce_urls` (below) controls. The indexer
caches each claim for about an hour, after which it looks the claim up in IPNI,
so an indexer without IPNI advertising serves a blob only for the first hour
after it is written. Piri logs a warning at startup for that combination.

A `network` preset fills in both keys, so under a preset the override has to be
explicit:

```toml
[ucan.services.indexer]
did = ""
url = ""
```

### [ucan.services.upload]

```toml
[ucan.services.upload]
did = "did:web:up.forge.storacha.network"
url = "https://up.forge.storacha.network"
```

### [ucan.services.etracker]

Egress tracker for usage reporting.

| Key | Default | Env | Dynamic |
|-----|---------|-----|---------|
| `ucan.services.etracker.max_batch_size_bytes` | `10485760` | `PIRI_UCAN_SERVICES_ETRACKER_MAX_BATCH_SIZE_BYTES` | No |

Constraints: 10MiB - 1GiB. The key is optional: when omitted, `piri serve`
applies the 10MiB default shown above, while an explicit `0` makes the
retrieval journal fall back to its built-in batch size of 100MiB. Neither
disables egress tracking — the integration is disabled by leaving `did` and
`url` unset.

```toml
[ucan.services.etracker]
did = "did:web:etracker.forge.storacha.network"
url = "https://etracker.forge.storacha.network"
receipts_endpoint = "https://..."
max_batch_size_bytes = 104857600  # 100 MiB
proof = "..."  # Optional
```

### [ucan.services.publisher]

IPNI advertising. Each new batch of advertisements is announced to every URL
listed; the IPNI nodes then sync the advertisement chain from this node at
`/ipni/v1/ad/:ad`.

```toml
[ucan.services.publisher]
ipni_announce_urls = [
  "https://cid.contact/announce",
  "https://ipni.forge.storacha.network"
]
```

An empty list turns IPNI advertising off. An IPNI node learns of the chain only
through an announce, so without one nothing would read it. Piri then builds no
advertisements, does not open the advertisement store under
`<data_dir>/publisher/`, does not run the `IPNIPublish` task and does not serve
`/ipni/v1/ad/:ad`. Location claims are still cached with the indexer when one is
configured. Piri logs `IPNI advertising is disabled` at startup.

The store grows by about 1.5 KB per accepted blob and is never pruned, which is
about 300 GB for 12 PiB of 64 MiB blobs. Turning advertising off stops that
growth.

A `network` preset fills in the list, so under a preset turn advertising off in
the config file. An empty `PIRI_IPNI_ANNOUNCE_URLS` counts as unset and keeps
the preset's URLs.

```toml
[ucan.services.publisher]
ipni_announce_urls = []
```

With advertising off, an existing `<data_dir>/publisher/` directory is unused
and you may delete it while Piri is stopped. Advertisements that were queued
and not yet published stay in the `ipni_pending_adverts` table, and the
`ipni_pending_adverts` metrics keep reporting them.

Turning advertising back on publishes the blobs accepted from then on. Blobs
accepted while it was off are not advertised until their advertisements are
rebuilt from the accepted blobs recorded in Postgres; Piri does not ship that
backfill yet. If `<data_dir>/publisher/` was kept, the chain continues from its
old head; if it was deleted, Piri starts a new chain.

### [ucan.services.principal_mapping]

Maps service DIDs to principal DIDs.

```toml
[ucan.services]
principal_mapping = { "did:web:up.forge.storacha.network" = "did:key:z6Mk..." }
```

</details>
