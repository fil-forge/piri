package chainrpc

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	filtypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// newTestMetrics returns Metrics on a meter whose readings the test collects.
func newTestMetrics(t *testing.T) (*Metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	m, err := NewMetrics(provider.Meter("test"))
	require.NoError(t, err)
	return m, reader
}

// collect returns every data point of the named instrument as a map from its
// attribute values, joined with "/" in attribute-key order, to its value.
func collect(t *testing.T, reader *sdkmetric.ManualReader, name string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			var points []metricdata.DataPoint[int64]
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				points = data.DataPoints
			case metricdata.Gauge[int64]:
				points = data.DataPoints
			default:
				t.Fatalf("%s has unexpected data type %T", name, m.Data)
			}
			for _, dp := range points {
				var values []string
				for _, kv := range dp.Attributes.ToSlice() {
					values = append(values, kv.Value.AsString())
				}
				out[strings.Join(values, "/")] = dp.Value
			}
		}
	}
	return out
}

// fakeLotus answers the Lotus calls Piri makes. heads, when set, is the
// channel ChainNotify returns.
type fakeLotus struct {
	headErr error
	heads   chan []*api.HeadChange
}

func (f *fakeLotus) ChainHead(context.Context) (*filtypes.TipSet, error) { return nil, f.headErr }

func (f *fakeLotus) ChainNotify(context.Context) (<-chan []*api.HeadChange, error) {
	return f.heads, nil
}

func (f *fakeLotus) StateGetRandomnessDigestFromBeacon(context.Context, abi.ChainEpoch, filtypes.TipSetKey) (abi.Randomness, error) {
	return nil, nil
}

// fakeEth answers BlockNumber; any other method panics through the nil
// embedded interface, which a test calling it would show at once.
type fakeEth struct {
	ethchain.EthClient
	blockNumberErr error
}

func (f *fakeEth) BlockNumber(context.Context) (uint64, error) { return 1, f.blockNumberErr }

func TestLotusClient_RecordsCalls(t *testing.T) {
	t.Run("counts each call by client, method and outcome", func(t *testing.T) {
		m, reader := newTestMetrics(t)
		lotus := &fakeLotus{}
		c := NewLotusClient(lotus, m)

		_, _ = c.ChainHead(context.Background())
		lotus.headErr = context.DeadlineExceeded
		_, _ = c.ChainHead(context.Background())
		_, _ = c.ChainHead(context.Background())

		require.Equal(t, map[string]int64{
			"lotus/ChainHead/ok":      1,
			"lotus/ChainHead/timeout": 2,
		}, collect(t, reader, "piri_chain_rpc_calls"))
	})
}

func TestEthClient_RecordsCalls(t *testing.T) {
	t.Run("counts each call under its Go method name", func(t *testing.T) {
		m, reader := newTestMetrics(t)
		c := NewEthClient(&fakeEth{blockNumberErr: errors.New("execution reverted")}, m)

		_, err := c.BlockNumber(context.Background())

		require.EqualError(t, err, "execution reverted")
		require.Equal(t, map[string]int64{"eth/BlockNumber/error": 1}, collect(t, reader, "piri_chain_rpc_calls"))
	})
}

func TestLotusClient_ChainNotify(t *testing.T) {
	t.Run("forwards every head change in order", func(t *testing.T) {
		m, _ := newTestMetrics(t)
		lotus := &fakeLotus{heads: make(chan []*api.HeadChange, 2)}
		first, second := []*api.HeadChange{{Type: "current"}}, []*api.HeadChange{{Type: "apply"}}
		lotus.heads <- first
		lotus.heads <- second
		close(lotus.heads)

		out, err := NewLotusClient(lotus, m).ChainNotify(context.Background())
		require.NoError(t, err)

		var got [][]*api.HeadChange
		for hc := range out {
			got = append(got, hc)
		}
		require.Equal(t, [][]*api.HeadChange{first, second}, got)
	})

	t.Run("counts a channel that closes while the caller still reads it", func(t *testing.T) {
		m, reader := newTestMetrics(t)
		lotus := &fakeLotus{heads: make(chan []*api.HeadChange)}
		out, err := NewLotusClient(lotus, m).ChainNotify(context.Background())
		require.NoError(t, err)

		close(lotus.heads)
		for range out {
		}

		require.Equal(t, map[string]int64{"lotus/ChainNotify": 1}, collect(t, reader, "piri_chain_rpc_subscriptions_closed"))
	})

	t.Run("does not count a channel closed because the caller cancelled", func(t *testing.T) {
		m, reader := newTestMetrics(t)
		lotus := &fakeLotus{heads: make(chan []*api.HeadChange)}
		ctx, cancel := context.WithCancel(context.Background())
		out, err := NewLotusClient(lotus, m).ChainNotify(ctx)
		require.NoError(t, err)

		// go-jsonrpc closes the channel once the call's context is done.
		cancel()
		close(lotus.heads)
		for range out {
		}

		require.Empty(t, collect(t, reader, "piri_chain_rpc_subscriptions_closed"))
	})
}

func TestProbe(t *testing.T) {
	t.Run("marks a client reachable when its probe call succeeds and unreachable when it fails", func(t *testing.T) {
		m, reader := newTestMetrics(t)
		eth := NewEthClient(&fakeEth{}, m)
		lotus := NewLotusClient(&fakeLotus{headErr: context.DeadlineExceeded}, m)

		probeOnce(context.Background(), m, eth, lotus)

		require.Equal(t, map[string]int64{"eth": 1, "lotus": 0}, collect(t, reader, "piri_chain_rpc_reachable"))
	})

	t.Run("counts the probe's calls like any other", func(t *testing.T) {
		m, reader := newTestMetrics(t)
		eth := NewEthClient(&fakeEth{}, m)
		lotus := NewLotusClient(&fakeLotus{headErr: context.DeadlineExceeded}, m)

		probeOnce(context.Background(), m, eth, lotus)

		require.Equal(t, map[string]int64{
			"eth/BlockNumber/ok":      1,
			"lotus/ChainHead/timeout": 1,
		}, collect(t, reader, "piri_chain_rpc_calls"))
	})
}
