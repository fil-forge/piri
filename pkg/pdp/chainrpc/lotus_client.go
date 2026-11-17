package chainrpc

import (
	"context"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	filtypes "github.com/filecoin-project/lotus/chain/types"
)

// LotusAPI is the part of the Lotus API Piri calls, the methods of
// service.ChainClient.
type LotusAPI interface {
	ChainHead(ctx context.Context) (*filtypes.TipSet, error)
	ChainNotify(ctx context.Context) (<-chan []*api.HeadChange, error)
	StateGetRandomnessDigestFromBeacon(ctx context.Context, randEpoch abi.ChainEpoch, tsk filtypes.TipSetKey) (abi.Randomness, error)
}

// LotusClient is a LotusAPI with every call recorded in Metrics.
type LotusClient struct {
	next    LotusAPI
	metrics *Metrics
}

// NewLotusClient wraps next so its calls are recorded in metrics. A nil
// metrics records nothing.
func NewLotusClient(next LotusAPI, metrics *Metrics) *LotusClient {
	return &LotusClient{next: next, metrics: metrics}
}

func (c *LotusClient) ChainHead(ctx context.Context) (*filtypes.TipSet, error) {
	ts, err := c.next.ChainHead(ctx)
	c.metrics.recordCall(ctx, ClientLotus, "ChainHead", err)
	return ts, err
}

// ChainNotify forwards the subscription's head changes, and counts the
// channel closing while ctx is still live: go-jsonrpc closes it when the
// WebSocket drops, and the caller then has to subscribe again.
func (c *LotusClient) ChainNotify(ctx context.Context) (<-chan []*api.HeadChange, error) {
	in, err := c.next.ChainNotify(ctx)
	c.metrics.recordCall(ctx, ClientLotus, "ChainNotify", err)
	if err != nil {
		return nil, err
	}
	out := make(chan []*api.HeadChange, cap(in))
	go func() {
		defer close(out)
		for hc := range in {
			select {
			case out <- hc:
			case <-ctx.Done():
				// The reader has gone; keep draining so go-jsonrpc is not
				// left blocked on a full channel.
			}
		}
		if ctx.Err() == nil {
			c.metrics.recordSubscriptionClosed(ClientLotus, "ChainNotify")
		}
	}()
	return out, nil
}

func (c *LotusClient) StateGetRandomnessDigestFromBeacon(ctx context.Context, randEpoch abi.ChainEpoch, tsk filtypes.TipSetKey) (abi.Randomness, error) {
	r, err := c.next.StateGetRandomnessDigestFromBeacon(ctx, randEpoch, tsk)
	c.metrics.recordCall(ctx, ClientLotus, "StateGetRandomnessDigestFromBeacon", err)
	return r, err
}
