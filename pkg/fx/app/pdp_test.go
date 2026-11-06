package app

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"

	"github.com/fil-forge/piri/pkg/config/app"
)

// refusingEndpoint serves a chain endpoint that refuses every WebSocket
// handshake with status, and returns its address carrying a token in the
// query string, the way Chain.Love takes its key.
func refusingEndpoint(t *testing.T, status int) (endpoint *url.URL, host string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	endpoint, err = url.Parse("ws://" + u.Host + "/ws?token=any-token")
	require.NoError(t, err)
	return endpoint, u.Host
}

func TestProvideLotusClient_RefusedHandshake(t *testing.T) {
	for _, tc := range []struct {
		status int
		text   string
	}{
		{http.StatusUnauthorized, "401 Unauthorized"},
		{http.StatusTooManyRequests, "429 Too Many Requests"},
	} {
		t.Run("reports HTTP status "+tc.text+" without the endpoint's query", func(t *testing.T) {
			endpoint, host := refusingEndpoint(t, tc.status)

			_, err := ProvideLotusClient(fxtest.NewLifecycle(t), app.PDPServiceConfig{LotusEndpoint: endpoint})

			require.EqualError(t, err, "providing lotus client: cannot dial address ws://"+host+"/ws for websocket: bad handshake: websocket: bad handshake (HTTP status "+tc.text+")")
		})
	}
}

func TestProvideEthClient_RefusedHandshake(t *testing.T) {
	for _, tc := range []struct {
		status int
		text   string
	}{
		{http.StatusUnauthorized, "401 Unauthorized"},
		{http.StatusTooManyRequests, "429 Too Many Requests"},
	} {
		t.Run("reports HTTP status "+tc.text+" without the endpoint", func(t *testing.T) {
			endpoint, _ := refusingEndpoint(t, tc.status)

			_, err := ProvideEthClient(fxtest.NewLifecycle(t), app.PDPServiceConfig{LotusEndpoint: endpoint})

			require.EqualError(t, err, "providing eth client: websocket: bad handshake (HTTP status "+tc.text+")")
		})
	}
}

func TestRedactEndpoint(t *testing.T) {
	t.Run("keeps scheme, host and path and drops userinfo, query and fragment", func(t *testing.T) {
		endpoint, err := url.Parse("wss://any-user:any-password@chain.example.com/ws/rpc/v1?token=any-token#any-fragment")
		require.NoError(t, err)

		require.Equal(t, "wss://chain.example.com/ws/rpc/v1", redactEndpoint(endpoint).String())
	})
}
