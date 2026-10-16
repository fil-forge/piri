# Observability

Contributor guidance for making a change observable. This file sits outside `docs/content/`, so it
is not part of the published operator site.

## Adding a signal

When a change adds or alters behavior an operator depends on, decide how they would see it working
and see it fail, and add that in the same PR:

- **Metrics and traces.** Instrument with OpenTelemetry in the package's `telemetry.go` or
  `*_metrics.go` (for example `pkg/curiopdp/metrics.go`, `lib/jobqueue/worker/telemetry.go`).
  Prefer a signal that answers "is this working?", such as a failure count or the time of the last
  success, over one that only measures volume. Keep attributes low-cardinality: no CIDs, DIDs or
  other per-request values.
- **Docs.** List each new metric in `docs/content/concepts/telemetry.md`, and in the "Key Metrics"
  table of `docs/content/operator-guide/monitoring.md` if operators should watch it. If it shows a
  failure an operator should act on, add a recommended alert with its severity to that page's
  "Alerts to Configure" table, and its query to the PromQL block with the others.
- **Forge's alerts and dashboards** live in fil-forge/infra-central, under
  `terraform/envs/grafana/`. A new metric does nothing for Forge's on-call until a rule or panel
  there reads it, so open a companion PR there, or say in this PR that one is needed.

Skip this for changes nothing operational depends on.
