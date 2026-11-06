package chainrpc

import (
	"context"
	"time"

	filtypes "github.com/filecoin-project/lotus/chain/types"
)

const (
	// probeInterval keeps an idle Piri reporting. Two calls a minute is a
	// small share of a hosted endpoint's per-key limit (Chain.Love's default
	// is 200 a minute).
	probeInterval = 30 * time.Second
	probeTimeout  = 10 * time.Second
)

// The probe makes one cheap read through each client.
type ethProber interface {
	BlockNumber(ctx context.Context) (uint64, error)
}

type lotusProber interface {
	ChainHead(ctx context.Context) (*filtypes.TipSet, error)
}

// StartProbe probes both clients now and every probeInterval until ctx is
// done. Pass the instrumented clients, so the probe's calls are counted too.
func StartProbe(ctx context.Context, m *Metrics, eth ethProber, lotus lotusProber) {
	go func() {
		ticker := time.NewTicker(probeInterval)
		defer ticker.Stop()
		for {
			probeOnce(ctx, m, eth, lotus)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// probeOnce sets each client's reachable gauge from one call. A call cut off
// by ctx ending (shutdown) records nothing.
func probeOnce(ctx context.Context, m *Metrics, eth ethProber, lotus lotusProber) {
	ethCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	_, err := eth.BlockNumber(ethCtx)
	cancel()
	if ctx.Err() != nil {
		return
	}
	m.recordReachable(ClientEth, err)

	lotusCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	_, err = lotus.ChainHead(lotusCtx)
	cancel()
	if ctx.Err() != nil {
		return
	}
	m.recordReachable(ClientLotus, err)
}
