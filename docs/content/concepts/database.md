# Database

Piri uses a PostgreSQL database to manage operational state, job queues, and the PDP proving pipeline. This page explains the database architecture.

PostgreSQL is the only supported backend: the PDP pipeline is built on Curio's harmonydb, which requires it.

## Logical Databases

Piri maintains three logical databases, each serving a distinct purpose:

| Database | Purpose |
|----------|---------|
| **PDP Pipeline** | Curio's harmonydb: proving-period scheduling, proof tasks, on-chain message tracking |
| **Replicator** | Data replication job tracking |
| **Egress Tracker** | Data egress operation tracking |

## Schema Layout

All three logical databases share one PostgreSQL database, isolated by schema:

- `curio` schema (PDP pipeline)
- `replicator` schema
- `egress_tracker` schema

Piri creates the schemas and applies migrations automatically on startup.

## PDP Aggregates

The aggregation pipeline keeps each PDP aggregate on the `pdp_blob_pipeline` rows of its pieces: `aggregate_root` names the aggregate, `agg_position` is the piece's position in it, and `inclusion_proof` holds the piece's inclusion proof. The columns are set in the transaction that folds the pieces, and the rows are deleted once the aggregate's pieces are added on-chain.

Earlier versions stored aggregates in a LevelDB store under `{data_dir}/aggregator`. Piri no longer opens that directory, and operators can delete it after upgrading. On upgrade, every aggregate that is not yet retired is folded again from its pipeline rows. An aggregate whose AddRoots transaction landed before the upgrade but whose rows were not yet retired is added on-chain a second time.

## Characteristics

- **Connection Pooling**: Configurable max connections and idle pool size
- **Concurrent Writers**: Supports multiple simultaneous writers
- **External Management**: Database runs separately from Piri; you are responsible for provisioning, backups, and upgrades
- **One Instance per Database**: Each Piri instance needs its own PostgreSQL database. Two Piri processes must never run against the same database, including during an upgrade.

## Configuration

See [Database Configuration](../configuration/repo/database.md) for PostgreSQL setup.
