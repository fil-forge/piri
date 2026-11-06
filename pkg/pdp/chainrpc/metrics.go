// Package chainrpc records how Piri's calls to its chain RPC endpoint fare:
// every call through the Lotus and eth clients by method and outcome, the
// Lotus ChainNotify subscription closing under its reader, and whether a probe
// call made every interval succeeds, so a Piri with no chain work to do still
// reports a key that stopped working or an endpoint that went away.
package chainrpc

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// The client attribute: which of Piri's two connections to the endpoint.
const (
	ClientLotus = "lotus"
	ClientEth   = "eth"
)

// Metrics holds the chain RPC instruments. A nil *Metrics records nothing.
type Metrics struct {
	calls     metric.Int64Counter
	closed    metric.Int64Counter
	reachable metric.Int64Gauge
}

// NewMetrics creates the chain RPC instruments on meter.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	calls, err := meter.Int64Counter(
		"piri_chain_rpc_calls",
		metric.WithDescription("Calls to the chain RPC endpoint, by client, method and outcome"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create chain rpc calls counter: %w", err)
	}
	closed, err := meter.Int64Counter(
		"piri_chain_rpc_subscriptions_closed",
		metric.WithDescription("Chain RPC subscriptions that closed while Piri was still reading them, by client and method"),
		metric.WithUnit("{subscription}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create chain rpc subscriptions closed counter: %w", err)
	}
	reachable, err := meter.Int64Gauge(
		"piri_chain_rpc_reachable",
		metric.WithDescription("1 when the last probe call through the client succeeded, 0 when it failed"),
	)
	if err != nil {
		return nil, fmt.Errorf("create chain rpc reachable gauge: %w", err)
	}
	return &Metrics{calls: calls, closed: closed, reachable: reachable}, nil
}

// recordCall counts one call. The context is detached from cancellation so a
// call cancelled by its caller is still counted.
func (m *Metrics) recordCall(ctx context.Context, client, method string, err error) {
	if m == nil {
		return
	}
	m.calls.Add(context.WithoutCancel(ctx), 1, metric.WithAttributes(
		attribute.String("client", client),
		attribute.String("method", method),
		attribute.String("outcome", string(classifyOutcome(err))),
	))
}

// recordSubscriptionClosed counts one subscription that closed under its reader.
func (m *Metrics) recordSubscriptionClosed(client, method string) {
	if m == nil {
		return
	}
	m.closed.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("client", client),
		attribute.String("method", method),
	))
}

// recordReachable sets the client's reachable gauge from its probe call.
func (m *Metrics) recordReachable(client string, err error) {
	if m == nil {
		return
	}
	var v int64
	if err == nil {
		v = 1
	}
	m.reachable.Record(context.Background(), v, metric.WithAttributes(attribute.String("client", client)))
}
