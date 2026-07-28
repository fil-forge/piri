# Agent Guidelines

## What this repo is

Piri (Provable Information Retention Interface) is a Go storage node for the
Storacha network. It stores blobs, proves continued possession via PDP
(proof-of-data-possession) against Ethereum/Filecoin smart contracts, serves
retrievals, replicates data between nodes, publishes IPNI advertisements, and
tracks egress. It interoperates with the indexing service and upload service.

Module path: `github.com/fil-forge/piri`. Go 1.25.

## Build, test, run

```bash
make build          # go build -o ./piri ./cmd (version info injected via ldflags)
make test           # go test ./...
./piri serve full   # start the full server
```

CLI is cobra + viper (`cmd/cli/`), with subcommands: `serve`, `setup`,
`status`, `client`, `identity`, `wallet`. The `--network` flag on
`serve full` selects a preset (`pkg/presets/`) that sets default service
URLs, DIDs, and contract addresses. Some tests use testcontainers
(Postgres, MinIO) and require Docker.

## Architecture

Dependency injection is uber/fx throughout.

- `pkg/fx/app/full.go` — `FullServerModule` is the single source of truth for
  the full-server dependency graph: `CommonModules` (identity, HTTP server,
  databases/datastores) + `UCANModule` (UCAN handlers, blob/claim/publisher/
  replicator/storage services) + `PDPModule` (lotus/eth/contract clients,
  piece aggregator, task + chain schedulers, PDP HTTP API, wallet).
- `pkg/fx/store/` — storage backend variants: `memory`, `filesystem`, `s3`.
- `pkg/fx/` also holds `database`, `echo`, `identity`, `pdp`, `root`,
  `scheduler` modules.

### Key paths

```
cmd/                  # entry point + cobra CLI (cmd/cli/serve/full.go)
pkg/ucanhandlers/     # per-capability UCAN handlers + fx registration helpers
  blob/               # blob/accept, blob/allocate, blob/retrieve, replica/
  access/             # access/grant
  content/            # content/retrieve
  pdp/                # pdp/info
  ucanfxtest/         # test harnesses for RPC and retrieval handlers
pkg/service/          # claims, egresstracker, proofs, publisher (IPNI),
                      # replicator, signer
pkg/pdp/              # PDP pipeline: aggregation (commp, aggregator, manager),
                      # chainsched, ethereum, httpapi, piece, promise, proof,
                      # scheduler, smartcontracts, tasks
pkg/config/           # config structs, validation, repo handling, presets glue
pkg/store/            # store implementations behind pkg/fx/store
lib/jobqueue/         # SQL-backed job queue (async work: replication, etc.)
internal/ipldstore/   # IPLD store internals
deploy/full-node/     # deployment assets; Dockerfile at repo root
docs/                 # mkdocs operator documentation (content/ + mkdocs.yml)
```

## UCAN stack

Piri uses `github.com/fil-forge/ucantone` (UCAN server/execution runtime) and
`github.com/fil-forge/libforge` (shared command/capability definitions under
`libforge/commands/...`, identity, retrieval protocol). There are two UCAN
servers with distinct response shapes:

- **RPC server** (`server.NewHTTP`, body-CAR): invocations whose response is a
  UCAN container of receipts — e.g. `blob/accept`, `blob/allocate`,
  `access/grant`, `pdp/info`, replica handlers.
- **Retrieval server** (`retrieval.NewServer`, header-container): invocations
  whose response body is a raw byte stream — e.g. `blob/retrieve`,
  `content/retrieve`.

Handlers register through fx group tags. Each handler package has a
`module.go` that wraps constructors with `ucanhandlers.ProvideRPC` /
`ProvideRetrieval` (and `ProvideRPCOption` / `ProvideRetrievalOption` for
server options). The group-tag strings live in `pkg/ucanhandlers/handler.go`
and appear in both the struct tags and helpers — keep them in sync.

To add a capability handler: define/import the command from libforge, write
the handler in the appropriate `pkg/ucanhandlers/<domain>/` package, register
it in that package's `module.go`, and add coverage via the `ucanfxtest`
harnesses.

## Dependency graph validation

`pkg/fx/app/full_test.go` validates the full-server fx dependency graph via
`fx.ValidateApp` across every branch of `store.StorageModule` (in-memory,
filesystem, S3).

Whenever a change to config options alters the dependency graph — e.g. adding a
new branch to `store.StorageModule`, gating a provider on a config value, or
introducing a config-driven module — update `pkg/fx/app/full_test.go` so the
new wiring is exercised. Add or adjust the storage/config variants in the test
to cover the new graph shape.

## Conventions

- Go files: snake_case. Tests: testify (`assert`/`require`).
- Logging: `github.com/ipfs/go-log/v2` (`log.Infow`, `log.Warnw`); fx event
  logs go through zap.
- HTTP: echo v4. Observability: OpenTelemetry (traces + metrics, otelecho
  middleware); per-package `telemetry.go` files add handler instrumentation.
- Persistence: PostgreSQL only (the PDP pipeline runs on Curio's harmonydb);
  job queues in `lib/jobqueue` back async processing.

## Gotchas / blast radius

- `blob/accept` kicks off the whole PDP pipeline — CommP calculation, piece
  aggregation into proof sets, and on-chain interaction. Changes there cascade
  through `pkg/pdp/`.
- Capability/command schemas come from `libforge`; they must stay
  wire-compatible with the other services that speak these protocols. Don't
  change invocation/receipt shapes unilaterally — coordinate with libforge and
  the consuming services.
- Retrieval handler changes affect live client content access.
- Config changes frequently change the fx graph — see the dependency graph
  validation section above.
