# Agent Guidelines

## Dependency graph validation

`pkg/fx/app/full_test.go` validates the full-server fx dependency graph via
`fx.ValidateApp` across every branch of `store.StorageModule` (in-memory,
filesystem, S3).

Whenever a change to config options alters the dependency graph — e.g. adding a
new branch to `store.StorageModule`, gating a provider on a config value, or
introducing a config-driven module — update `pkg/fx/app/full_test.go` so the
new wiring is exercised. Add or adjust the storage/config variants in the test
to cover the new graph shape.
