# Telemetry

Piri uses [OpenTelemetry](https://opentelemetry.io/) to emit metrics and
traces about itself. **Piri sends this data nowhere by default.** It is
exported only to the collectors your configuration names, and a node with no
`[telemetry]` section starts no exporter at all.

Earlier versions of Piri reported usage analytics to an endpoint operated by
Storacha, the project Piri came from, unless the operator opted out. That
endpoint no longer exists and the default was removed, along with the
`disable_storacha_analytics` setting and the `PIRI_DISABLE_ANALYTICS`
environment variable that turned it off. Both are now ignored if present:
there is nothing left to opt out of.

## What a node emits

When you configure a collector, these are the things Piri reports about
itself. [Concepts > Telemetry](../concepts/telemetry.md) lists every metric
and span.

### Resource attributes

Attached to every metric and span:

- **Service name**: always `piri`
- **Service version**: the Piri version you are running
- **Service instance ID**: your node's DID
- **Deployment environment**: `telemetry.environment`, or the configured
  network

### Server information

Recorded once at startup as `piri_server_info`: version, commit, build date,
builder, start time, and server type. A PDP server also reports its Ethereum
address; a UCAN server reports its DID and the DIDs and public URLs of the
indexing and upload services it is configured against.

These identifiers are pseudonymous but they are part of your node's public
identity on the network, so treat the collector you send them to as you would
any other operational data store.

### Never emitted

- Private keys or any other cryptographic material
- The contents of stored data
- Client IP addresses or location data

## Configuring a collector

See [Configuration > telemetry](../configuration/telemetry.md) for the
settings, and [Operator Guide > Monitoring](../operator-guide/monitoring.md)
for what to watch.

## Technical implementation

- Exporters are built at startup, and only when a collector is configured.
- OpenTelemetry's own errors — an unreachable collector, a refused batch — are
  logged through Piri's logger under the `telemetry` subsystem at `warn`, rate
  limited to one line every five minutes with a count of what was suppressed.
- A failing exporter never stops the node; Piri logs and carries on.

## Questions or concerns

- Open an issue on our [GitHub repository](https://github.com/fil-forge/piri)
- Contact the development team through official channels
