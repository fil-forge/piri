package chainrpc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	filtypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/gorilla/websocket"
)

const (
	// probeInterval keeps an idle Piri reporting. Two calls a minute is a
	// small share of a hosted endpoint's per-key limit (Chain.Love's default
	// is 200 a minute).
	probeInterval = 30 * time.Second
	probeTimeout  = 10 * time.Second
	// handshakeInterval spaces the fresh handshakes. A WebSocket endpoint
	// checks the key only when a connection opens, so the calls above, on
	// connections opened at start-up, cannot see a key revoked since.
	handshakeInterval = 5 * time.Minute
)

// The probe makes one cheap read through each client.
type ethProber interface {
	BlockNumber(ctx context.Context) (uint64, error)
}

type lotusProber interface {
	ChainHead(ctx context.Context) (*filtypes.TipSet, error)
}

// StartProbe probes both clients now and every probeInterval, and opens a
// fresh handshake with endpoint now and every handshakeInterval, until ctx is
// done. Pass the instrumented clients, so the probe's calls are counted too.
func StartProbe(ctx context.Context, m *Metrics, eth ethProber, lotus lotusProber, endpoint Endpoint) {
	go func() {
		ticker := time.NewTicker(handshakeInterval)
		defer ticker.Stop()
		for {
			probeHandshake(ctx, m, endpoint)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
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

// Endpoint is where the clients connect, and the header they send, for the
// handshake probe.
type Endpoint struct {
	URL    *url.URL
	Header http.Header
}

// probeHandshake opens and closes one WebSocket connection to the endpoint and
// counts it as client "endpoint", method "Handshake", so a key the endpoint
// has stopped accepting shows as unauthorized while the clients' own
// connections still work. An HTTP endpoint checks the key on every call, so
// it gets no handshake. The endpoint's address never leaves this function.
func probeHandshake(ctx context.Context, m *Metrics, endpoint Endpoint) {
	if endpoint.URL == nil || (endpoint.URL.Scheme != "ws" && endpoint.URL.Scheme != "wss") {
		return
	}
	dialCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	conn, resp, err := websocket.DefaultDialer.DialContext(dialCtx, endpoint.URL.String(), endpoint.Header)
	if conn != nil {
		_ = conn.Close()
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if ctx.Err() != nil {
		return
	}
	// Put the status where classifyOutcome reads it, as go-ethereum does.
	if errors.Is(err, websocket.ErrBadHandshake) && resp != nil {
		err = fmt.Errorf("%w (HTTP status %s)", err, resp.Status)
	}
	m.recordCall(ctx, ClientEndpoint, "Handshake", err)
}
