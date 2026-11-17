package chainrpc

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func parseEndpoint(t *testing.T, raw string) Endpoint {
	t.Helper()
	u, err := url.Parse(raw + "/ws?token=any-token")
	require.NoError(t, err)
	return Endpoint{URL: u}
}

func TestProbeHandshake(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   map[string]int64
	}{
		{"counts a handshake the endpoint refuses with 401 as unauthorized", http.StatusUnauthorized, map[string]int64{"endpoint/Handshake/unauthorized": 1}},
		{"counts a handshake the endpoint refuses with 429 as rate limited", http.StatusTooManyRequests, map[string]int64{"endpoint/Handshake/rate_limited": 1}},
		{"counts a handshake the endpoint accepts as ok", 0, map[string]int64{"endpoint/Handshake/ok": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, reader := newTestMetrics(t)

			probeHandshake(context.Background(), m, parseEndpoint(t, serveEndpoint(t, endpoint{handshakeStatus: tc.status}, "ws")))

			require.Equal(t, tc.want, collect(t, reader, "piri_chain_rpc_calls"))
		})
	}

	t.Run("makes no handshake for an HTTP endpoint", func(t *testing.T) {
		m, reader := newTestMetrics(t)

		probeHandshake(context.Background(), m, parseEndpoint(t, serveEndpoint(t, endpoint{}, "http")))

		require.Empty(t, collect(t, reader, "piri_chain_rpc_calls"))
	})
}
