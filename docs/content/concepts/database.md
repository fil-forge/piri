# Database

Piri uses a PostgreSQL database to manage operational state, job queues, and the PDP proving pipeline. This page explains the database architecture.

> **Note**: PostgreSQL is the only supported backend. Earlier versions of Piri supported SQLite, but the PDP pipeline (built on Curio's harmonydb/harmonytask) requires PostgreSQL, and SQLite support has been removed.

## Logical Databases

Piri maintains several logical databases, each serving a distinct purpose:

| Database | Purpose |
|----------|---------|
| **PDP Pipeline** | Curio's harmonydb: proving-period scheduling, proof tasks, on-chain message tracking |
| **Replicator** | Data replication job tracking |
| **Aggregator** | CommP hash aggregation job queue |
| **Egress Tracker** | Data egress operation tracking |

## Schema Layout

All logical databases share one PostgreSQL database, isolated by schema:

- `curio` schema (PDP pipeline / harmonydb)
- `replicator` schema
- `aggregator` schema
- `egress_tracker` schema

Piri creates the schemas and applies migrations automatically on startup.

## Characteristics

- **Connection Pooling**: Configurable max connections and idle pool size
- **Concurrent Writers**: Supports multiple simultaneous writers
- **External Management**: Database runs separately from Piri; you are responsible for provisioning, backups, and upgrades

## Configuration

See [Database Configuration](../configuration/repo/database.md) for PostgreSQL setup.
