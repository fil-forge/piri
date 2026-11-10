# Store Package

This package provides storage abstractions for Piri. Stores are composed in layers, with different patterns for structured data vs binary blobs.

## Architecture

```
                    ┌─────────────────────────────────────────────────┐
                    │              Application Layer                  │
                    └─────────────────────────────────────────────────┘
                                          │
              ┌───────────────────────────┴───────────────────────────┐
              │                                                       │
              ▼                                                       ▼
┌─────────────────────────────┐                         ┌─────────────────────────┐
│   genericstore.Store[T]     │                         │    blobstore.Store      │
│   ─────────────────────     │                         │    ───────────────      │
│   • Codec[T] serialization  │                         │    • KeyEncoder only    │
│   • Full buffering (ReadAll)│                         │    • Streaming preserved│
│   • For small structured    │                         │    • Range requests     │
│     records (KB-sized)      │                         │    • For large binaries │
└─────────────────────────────┘                         │     (100s of MiB)       │
              │                                         └─────────────────────────┘
              │                                                       │
              └───────────────────────────┬───────────────────────────┘
                                          │
                                          ▼
                            ┌─────────────────────────────┐
                            │      objectstore.Store      │
                            │      ────────────────       │
                            │   • Get() → Object (stream) │
                            │   • Put(io.Reader)          │
                            │   • Delete()                │
                            │   • List()                  │
                            └─────────────────────────────┘
                                          │
              ┌───────────────────────────┼───────────────────────────┐
              │                           │                           │
              ▼                           ▼                           ▼
    ┌─────────────────┐         ┌─────────────────┐         ┌─────────────────┐
    │   minio.Store   │         │  flatfs.Store   │         │  memory.Store   │
    │   (S3/MinIO)    │         │  (filesystem)   │         │  dsadapter      │
    └─────────────────┘         └─────────────────┘         └─────────────────┘
```

All blobstore implementations use a single `blobstore.Store` type that wraps different objectstore backends:

```go
// Production backends
blobstore.NewS3Store(minioStore)      // S3/MinIO
blobstore.NewFlatfsStore(flatfsStore) // Filesystem

// Testing backends
blobstore.NewMemoryStore()            // In-memory map
blobstore.NewDatastoreStore(ds)       // IPFS datastore
```

## Store Patterns

### Record Stores (Postgres)

The per-blob records the UCAN handlers keep live in tables in the node's
harmonydb Postgres database (migration
`pkg/curiopdp/schema/sql/00000007-ucan-records.sql`), whichever backend holds
the blobs. Each store keeps its interface, a `NewPostgresStore(db)` used by the
node (`pkg/fx/store/postgres`), and a `NewDatastoreStore(ds)` over
`genericstore` for in-memory use and tests.

| Store | Record | Table |
|-------|--------|-------|
| `allocationstore` | `Allocation`, `Pending` | `ucan_allocations`, `ucan_pending_allocations` |
| `acceptancestore` | `Acceptance` | `ucan_acceptances` |
| `invocationstore` | `ucan.Invocation` (location commitments, the claim store) | `ucan_invocations` |
| `receiptstore` | `ucan.Receipt`, found by CID or by the task it ran | `ucan_receipts`, `ucan_receipts_by_ran` |
| `consolidationstore` | `Consolidation` | `ucan_consolidations` |

`ipnistore` keeps the IPNI advertisement chain for go-ipni-tools' publisher
(migration `00000008-ipni-publisher-store.sql`): the signed head in
`ipni_head` and the per-context maps in `ipni_contexts`, and each advert as
an object keyed by its CID (`ipni/<cid>` in the S3 bucket,
`data_dir/ipni/<shard>/<cid>.data` on flatfs). Entries chunks are inlined
into identity CIDs. It is also the publisher's batch store: a publish batch
reads its contexts with one `SELECT`, uploads its adverts in parallel, and
commits its contexts with one upsert and the head with a compare-and-swap,
all in one transaction.
In-memory nodes use go-ipni-tools' datastore store instead.

Changes that read an allocation first lock its row, so they hold across
processes. Each store's tests run against both implementations
(`impls_test.go`); the Postgres runs skip on darwin.

### Binary Data Store (direct objectstore wrapper)

`blobstore.Store` wraps `objectstore.Store` directly to preserve streaming. It cannot use genericstore because:

1. Blobs are large (100s of MiB) - buffering would cause OOM
2. Range requests are required for partial reads
3. No serialization needed - data is stored as-is

The blobstore adds only a `KeyEncoder` for consistent key formatting across backends.

### Local-Only Stores (`local/`)

These stores are in the `local/` subdirectory because they are inherently filesystem-based and do not participate in S3/filesystem backend selection. They will never have cloud or Postgres implementations.

| Store | Backend | Notes |
|-------|---------|-------|
| `local/keystore` | LevelDB datastore | Cryptographic key storage, always on disk for security |
| `local/retrievaljournal` | Filesystem | Egress tracking journal with periodic GC, filesystem-only by design |

## Key Encoding

Both patterns use key encoding for backend compatibility:

- **S3/MinIO**: Base32-encoded multihash (lowercase, no prefix)
- **Filesystem**: Base32-encoded with sharding (NextToLast(2))
- **Memory/Datastore**: Plain formatted digest

## Backend Selection

Storage backend is selected via configuration:

- `storage.s3` configured → blobs on S3/MinIO (`pkg/fx/store/s3`), records in Postgres (`pkg/fx/store/postgres`)
- `storage.data_dir` configured → blobs on the filesystem (`pkg/fx/store/filesystem`), records in Postgres
- Neither configured → every store in memory (`pkg/fx/store/memory`)
