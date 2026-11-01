# Allocation Lifecycle: Capacity Gating, Expiry, and Finalization

- **Status:** Draft
- **Date:** 2026-07-29
- **Scope:** `blob/allocate`, `blob/accept`, the allocation store, the PDP upload pipeline, and the storage backends (flatfs, MinIO/S3, memory).

## 1. Background: how the flow works today

An allocation reserves space on a Piri node for a future upload.
The flow:

1. **`blob/allocate`** (`pkg/ucanhandlers/blob/allocate.go`) checks for an existing allocation for `(digest, space)`, then for the digest in any space, and asks the PDP layer whether the blob bytes are already present (`Pieces.Has`).
   If the bytes aren't present it calls `PDPService.AllocatePiece`, which inserts a row into `pdp_piece_uploads` (`pkg/pdp/service/piece_allocate.go`) and returns an upload UUID.
   The handler returns a `BlobAddress{URL, Expires}` pointing at `PUT /pdp/piece/upload/:uuid` and persists an `Allocation{Space, Blob, Expires, Cause}` record.
2. **Upload.**
   The client `PUT`s the bytes.
   `UploadPiece` (`pkg/pdp/service/piece_upload.go`) looks up the `pdp_piece_uploads` row, hash-verifies the stream, writes it to the blobstore (flatfs directory, MinIO `{prefix}pdp` bucket, or memory), and deletes the upload row.
3. **`blob/accept`** (`pkg/ucanhandlers/blob/accept.go`) verifies the bytes exist, enqueues CommP/aggregation, issues the location claim, publishes it, and records an `Acceptance` in the acceptance store.

### What exists but is unused or unenforced

- **Expiration is half-built.**
  `Allocation.Expires` is persisted and documented as "the time at which the allocation becomes invalid and can no longer be accepted" (`pkg/store/allocationstore/allocation/allocation.go`), and the wire type `blob.BlobAddress.Expires` is returned to clients.
  But the value is **hardcoded to 24h** (`allocate.go`), and *nothing reads it*: `allocationstore.GetAnyNonExpired` exists with zero production callers, the upload endpoint only checks that the `pdp_piece_uploads` row exists, and `blob/accept` does not consult the allocation store at all.
- **Nothing is cleaned up.**
  `AllocationStore` has no `Delete` method.
  Stale `pdp_piece_uploads` rows for never-completed uploads live forever, and each re-allocate of the same blob mints a *new* row, since `AllocatePiece` only dedups against completed pieces.
- **No space check anywhere** — not at allocate, not at upload.
  The only size gates are per-blob (`blob.MaxBlobSize`, `PieceSizeLimit`).

### A load-bearing subtlety

`content/retrieve` authorizes retrievals by looking up the allocation for `(digest, space)` (`pkg/ucanhandlers/content/retrieve.go`).
Allocations therefore double as the permanent "this space stores this blob here" record.
Since every allocation "expires" 24h after creation even when the blob was uploaded and accepted, **expired allocations cannot simply be deleted** — that would break retrieval authorization for successfully stored data.
This single fact shapes most of the design below.

## 2. Goals

1. Reject `blob/allocate` when the backing object store is above a configurable occupancy threshold (e.g. 80%), for both flatfs and MinIO/S3 backends.
2. Make allocation expiry real: configurable TTL, enforced at upload and accept time, with expired never-fulfilled allocations (and their upload slots) garbage-collected.

Non-goals, noted as follow-ups in §10: GC of uploaded-but-never-accepted blob bytes, per-space quotas, `blob/remove`-driven reclamation.

## 3. The allocation lifecycle model

The design introduces an explicit lifecycle for allocation records:

```
                 upload + blob/accept
   PENDING ───────────────────────────────► ACCEPTED (permanent)
      │                                        │
      │ Expires + grace passes                 │ future blob/remove
      ▼                                        ▼
   deleted by janitor                       deleted
```

- **Pending**: created by `blob/allocate`; carries an upload deadline (`Expires`).
  Grants nothing durable.
- **Accepted (finalized)**: `blob/accept` succeeded.
  The record is the durable per-space storage ledger entry and the `content/retrieve` authorization record.
  It no longer expires.

Today both states are smeared into one record shape with an `Expires` field nobody enforces.
§6 describes two ways to realize the split; the recommended one makes the state explicit on the record.

## 4. Capacity-aware allocation

### 4.1 Abstraction

Add a small capacity package (suggest `pkg/store/capacity`):

```go
// Usage is a snapshot of the blob store's capacity.
type Usage struct {
    Total   uint64 // volume size or configured capacity
    Used    uint64 // bytes currently stored
    Pending uint64 // bytes reserved by outstanding upload slots
}

// Monitor reports capacity for a storage backend.
type Monitor interface {
    Usage(ctx context.Context) (Usage, error)
}

// Guard decides whether a new allocation of size n may proceed.
type Guard interface {
    CheckAvailable(ctx context.Context, size uint64) error
}
```

The `Guard` composes a backend `Monitor`, a pending-bytes source, and the configured threshold: deny when `Used + Pending + size > threshold × Total`.

### 4.2 Per-backend monitors

Provided by the existing fx storage branches (`pkg/fx/store/{filesystem,s3,memory}`), so the right monitor is wired automatically per `store.StorageModule` branch:

- **Filesystem (flatfs):** `unix.Statfs` on the PDP store directory.
  `Total` = volume size, `Used = Total − Available`.
  This matches the natural operator meaning of "80% of disk occupied", costs nothing per call, and needs no bookkeeping.
  (Piri's vendored flatfs has no disk-usage tracking; none is needed.)
- **MinIO/S3:** the S3 API cannot report free space.
  Rather than pulling in `madmin-go` (admin credentials, MinIO-only) for v1, require an operator-declared capacity (`capacity_bytes`) and measure `Used` with a periodic bucket scan (`ListObjects` over the `{prefix}pdp` bucket, summing sizes), cached and refreshed on an interval (default ~5m).
  If `capacity_bytes` is unset, the check is disabled with a startup warning.
  A `madmin`-based monitor can be a later opt-in.
- **Memory:** no-op monitor (always allow).

**Pending bytes** come from the PDP database:

```sql
SELECT COALESCE(SUM(check_size), 0) FROM pdp_piece_uploads;
```

This is exactly the set of promised-but-not-yet-uploaded bytes (rows are deleted on successful upload), it is one cheap query, and it covers the replication sink path too, since replicas use the same upload slots.
This is also why upload-slot cleanup (§7) matters: without it, dead reservations permanently inflate `Pending` and eventually wedge allocation.

`Used` is deliberately *not* derived from `pdp_piece_mh_to_commp` — that table only covers commp-codec uploads, not the common sha2-256 blob path, so store-level measurement is the reliable source.

### 4.3 Enforcement point

In `Allocate`, after the dedup checks and only when new bytes are actually needed (`!received`), call `Guard.CheckAvailable(ctx, req.Blob.Size)` before `AllocatePiece`.
On denial, return a **named receipt failure**, mirroring the existing `BlobSizeLimitExceeded` pattern:

```go
const InsufficientCapacityErrorName = "InsufficientStorageCapacity"
```

ucantone failure receipts are `{name, message}`, so this needs **no libforge schema change**; the upload service just needs to know the name so it can route the upload to another node (coordination noted in §9).

There is a small TOCTOU window between concurrent allocates, but `AllocatePiece` inserts the `pdp_piece_uploads` row (which feeds `Pending`) immediately after the check, and the threshold itself provides headroom.
Accepted for v1; a reservation ledger can be added later if it proves insufficient.

## 5. Allocation expiry

### 5.1 Configurable TTL

Replace the hardcoded 24h in `Allocate` with `AllocationTTL` from config (default 24h, preserving current behavior).
It flows to both the persisted `Allocation.Expires` and the wire `BlobAddress.Expires`, so clients keep seeing an honest deadline.
No wire change.

### 5.2 Enforcement points

1. **Allocate (idempotency semantics):** treat expired, unfulfilled allocations as absent.
   The existing `Get`/`Exists` checks become expiry-aware (finally wiring in `GetAnyNonExpired`, plus a non-expired variant of the per-space `Get`), so a re-allocate after expiry issues a fresh upload slot, refreshes `Expires`, and reports full `Size` again rather than 0.
   Accepted records never count as expired (§6.2).
2. **Upload (`PUT /pdp/piece/upload/:uuid`):** `UploadPiece` rejects when `now > created_at + TTL` (the table already has `created_at`; no schema change to the Curio-derived migration set) and deletes the row, returning `410 Gone`.
   This is the enforcement that actually stops late writes from consuming disk.
   Note: enforcing from `created_at + TTL` means a TTL config change retroactively applies to open slots; documented and acceptable.
3. **Accept:** add the allocation store to `AcceptDeps` and require a valid (non-expired or accepted) allocation for `(digest, space)`.
   This makes the documented meaning of `Expires` ("can no longer be accepted") true, and is what makes janitor deletion safe to reason about.

## 6. Cleanup: two designs for the janitor

Both variants share the same skeleton: a periodic cleanup service (suggest `pkg/service/janitor`) modeled on the egress tracker's cleanup loop (`pkg/service/egresstracker/service.go`): fx lifecycle hooks + ticker, interval configurable, `0` disables.
Both need `List` and `Delete` added to the `AllocationStore` interface (both backends already sit on `ListableStore`, so this is prefix-list + delete).
They differ in how the janitor decides an allocation is safe to delete.

### 6.1 Option 1 (rejected): acceptance-aware janitor

For each allocation with `Expires + grace < now`, delete it **only if no acceptance exists for `(digest, space)`** in the acceptance store.

- **Pros:** no change to the stored record shape.
- **Cons:**
  - The allocation record's meaning stays muddy: an "expired" record may be live-and-permanent (accepted) or dead (unfulfilled), and only a *different store* can tell you which.
    Every future consumer of allocations inherits that cross-store dependency.
  - The janitor's fx graph grows an acceptance-store dependency, and the correctness argument is a cross-store read-then-delete: janitor reads "no acceptance", a slow in-flight accept writes the acceptance, janitor deletes the allocation → an accepted blob silently loses its `content/retrieve` authorization record.
    The window is bounded by `grace` (accept requires a non-expired allocation at its start, and the janitor only touches records `grace` past expiry), but it is a timing argument, not an invariant, and **both** orderings of the race lose: the acceptance write never touches the allocation store, so the janitor's delete always wins.

### 6.2 Option 2 (recommended): finalize the allocation at accept

Make `blob/accept` flip the allocation record itself into the accepted state.
The janitor then deletes any allocation that is *pending and expired* — a single-store decision made entirely from the record's own contents.

#### 6.2.1 Representing "accepted"

Two representations were considered:

- **(a) Sentinel:** on accept, re-`Put` the allocation with `Expires = 0`, documented as "never expires / finalized".
  No struct change, no codegen.
  Cost: overloads one field, destroys the original deadline, and cannot distinguish "finalized" from any future "never-expiring allocation" concept.
- **(b) Explicit field (recommended):** add a field to `allocation.Allocation`:

  ```go
  // AcceptedAt is the time (seconds since unix epoch) at which the
  // allocation was fulfilled via blob/accept. Zero means the allocation
  // is still pending and subject to Expires.
  AcceptedAt ucan.UnixTimestamp `cborgen:"acceptedAt" dagjsongen:"acceptedAt"`
  ```

  These records are piri-internal — they never cross the wire — so there is no cross-service compatibility concern.
  Regenerate via `pkg/store/allocationstore/allocation/gen`.
  The record becomes self-describing: `AcceptedAt == 0 && Expires < now` *is* the definition of dead, checkable by anyone holding the record.

The janitor rule becomes: delete iff `AcceptedAt == 0 && Expires + grace < now`.
The expiry predicates from §5.2 use the same definition ("valid" = `AcceptedAt != 0 || Expires > now`), so `GetAnyNonExpired` and friends treat accepted records as permanently valid.

#### 6.2.2 Changes to the accept flow

`Accept` (with the allocation store now in `AcceptDeps` per §5.2.3):

1. Load the allocation for `(digest, space)`; fail with a named error if missing or expired-and-unaccepted.
2. Proceed as today (piece check, commp enqueue, claims, acceptance-store put, publish).
3. Immediately after the acceptance-store `Put` succeeds, re-`Put` the allocation with `AcceptedAt = now`.
   If this write fails, the handler returns an error and the caller retries `blob/accept`; the flow is idempotent (re-accepting an already-accepted allocation just rewrites the same state).

The disabled `blob/replica/allocate`/`transfer` path must apply the same finalization when it is re-enabled — replica transfers conclude with the same `Accept` internals, so this falls out naturally if finalization lives in `blob.Accept` rather than the route wrapper.

Note the allocate handler already avoids clobbering accepted records in the common path: `allocated && received` returns early without re-`Put`ing.
The one edge — an accepted allocation whose bytes have vanished (a future GC or removal bug) — would cause allocate to reset the record to pending with a fresh deadline, which is exactly the recovery behavior wanted: re-upload, re-accept.

#### 6.2.3 Race analysis (and honesty about it)

The object stores backing allocations (leveldb via dsadapter, MinIO) offer no compare-and-delete, so *neither* option can make janitor-vs-accept atomically safe.
The honest comparison:

- **Option 1** loses under both interleavings of a straddling accept: the acceptance write never touches the allocation store, so a janitor delete based on a stale "no acceptance" read always wins, permanently.
- **Option 2** loses under only one interleaving.
  If the janitor deletes first and accept's finalize `Put` lands second, the record is *recreated in the accepted state* — last-writer-wins repairs the damage.
  Only the reverse order (janitor read stale pending record → accept finalizes → janitor deletes) loses, and it additionally requires an accept handler that has been running longer than `grace` (since accept must have seen `Expires > now` at its start and the janitor only touches records `Expires + grace < now`).

So both designs ultimately lean on `grace` exceeding the worst-case accept duration (default 1h vs. a handler that runs seconds), but option 2 halves the losing interleavings, repairs itself in the half it wins, and — more importantly — turns the janitor's correctness condition into a local, auditable property of one record in one store.

#### 6.2.4 What finalization buys beyond cleanup

- **`content/retrieve` tightening (optional follow-up).**
  Today any allocation authorizes space-scoped retrieval — including a never-fulfilled allocation created by a space that merely knew the digest, piggybacking on bytes another space uploaded.
  Expiry+cleanup closes that hole after TTL+grace; with finalization, retrieval can require an *accepted* allocation and close it exactly.
  (Needs a behavior-change sign-off, since it tightens live retrieval semantics.)
- **Alignment with blob removal.**
  libforge already defines `blob.RemoveArguments` (release a space's claim on a digest), and removal work is in flight across piri/libforge/sprue.
  Removal needs precisely a crisp per-space "stored" ledger: accepted allocations are that ledger, and removal becomes "delete the accepted allocation; if it was the last accepted allocation for the digest, the bytes are reclaimable."
  Pending records with nobody enforcing their meaning cannot serve that role.
- **Simpler dependency graph.**
  The steady-state janitor depends on one store, not two; the accept-time invariant is enforced where the state changes rather than reconstructed at cleanup time.

### 6.3 Recommendation

Adopt **option 2**.
There are no existing deployments to migrate, so the entire incremental cost over option 1 is one cborgen field and one extra `Put` in the accept flow.
Option 1's only virtue was avoiding a record change; without a compatibility constraint it is strictly worse — it leaves the ambiguous-record problem permanently in place, and every future consumer (removal, retrieval tightening, per-space accounting) would re-derive acceptance state across stores.

## 7. Upload-slot cleanup (both options)

Independent of the allocation-record design, each janitor tick asks the PDP service to reap dead upload reservations:

```sql
DELETE FROM pdp_piece_uploads WHERE created_at < now() - $ttl - $grace;
```

exposed as a narrow method on `PDPService` so the janitor never touches harmonydb directly.
Rows are deleted on successful upload, so anything older than `TTL + grace` is a dead reservation.
This keeps the capacity guard's `Pending` figure honest (§4.2).

## 8. Configuration

Following existing placement (`RepoConfig` owns storage/S3 in `[repo]`; `[ucan]` owns service behavior).
Exact key names to be settled during implementation:

```toml
[repo.capacity]
threshold      = 0.80            # deny allocations above this occupancy; 0 disables
capacity_bytes = 10_000_000_000  # required for S3/MinIO; optional override for filesystem
scan_interval  = "5m"            # S3 usage-scan cadence

[ucan.allocations]
ttl              = "24h"         # allocation / upload-slot lifetime
cleanup_interval = "1h"          # janitor cadence; 0 disables
cleanup_grace    = "1h"          # slack beyond ttl before deleting anything
```

Each gets `mapstructure`/`toml`/`flag` tags, validation, and `ToAppConfig()` mapping like `EgressTrackerServiceConfig`.
Defaults preserve today's client behavior (24h deadlines) while enabling the janitor and, on filesystem, the 80% gate; on S3 the gate stays off until `capacity_bytes` is set (with a startup warning).

## 9. Wire compatibility and cross-service coordination

- No libforge schema changes.
  `BlobAddress.Expires` is already on the wire; it simply becomes honest.
  Failure receipts are `{name, message}`.
- The upload service should learn the `InsufficientStorageCapacity` failure name and route uploads to another node; today an unknown failure name presumably surfaces as a generic error.
  Coordinate before shipping the gate with a low threshold.
- Stricter `blob/accept` (requires valid allocation) is a behavior change visible to the upload service in pathological flows (accept without prior allocate, accept long after the deadline).
  Flag in release notes.
- `content/retrieve` tightening (§6.2.4) is explicitly deferred and needs its own sign-off.

## 10. Phasing

1. **Expiry enforcement + janitor** (§5, §6, §7): TTL config, allocate / upload / accept enforcement, `AcceptedAt` field + finalization, janitor.
   No data-plane deletions beyond allocation records and upload-slot rows.
2. **Capacity gating** (§4): monitors, guard, allocate-time denial, config, upload-service coordination on the failure name.
3. **Follow-ups (each needs its own review):** GC of uploaded-but-never-accepted blob bytes (provably outside the aggregation pipeline, since CommP is enqueued only at accept — but it deletes data); `content/retrieve` accepted-only tightening; `madmin`-backed MinIO monitor; per-space quotas; `blob/remove` implementation on top of the accepted-allocation ledger.

## 11. Testing

- **fx graph:** new providers (`Monitor`, `Guard`, janitor) appear in every branch of `store.StorageModule` — extend `pkg/fx/app/full_test.go` to validate all three graph shapes, per the repo's dependency-graph rule.
- **Handler tests (`ucanfxtest`):** allocate denial via fake guard; expired-allocation re-allocate semantics (fresh slot, full `Size`); accept-after-expiry rejection; accept finalization writes `AcceptedAt`; re-accept idempotency.
- **Backend tests:** statfs monitor unit test; MinIO usage scan via the existing testcontainers setup; upload-slot TTL rejection (410) against the PDP service.
- **Janitor tests (memory stores):** pending+expired deleted; accepted survives regardless of `Expires`; stale `pdp_piece_uploads` reaping.

## 12. Open questions

1. **Strict vs lenient accept on the deadline edge.**
   With finalization the recommendation is strict (accept requires `AcceptedAt != 0 || Expires > now`), with `cleanup_grace` sized to cover normal upload→accept latency.
   The lenient variant (accept whenever the bytes exist and any allocation record exists) remains available if the upload service's accept latency turns out to be unbounded.
2. **Should the capacity threshold also gate the upload `PUT` itself?**
   Allocate-time denial is the contract, but a belt-and-braces check in `UploadPiece` would bound the worst case of many outstanding slots landing at once.
   Cheap to add if wanted.
