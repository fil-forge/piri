package chainrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	jsonrpc "github.com/filecoin-project/go-jsonrpc"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// endpoint is how the fake chain RPC endpoint answers.
type endpoint struct {
	// handshakeStatus, when set, refuses every request with this HTTP status.
	handshakeStatus int
	// rpcErrCode and rpcErrMessage, when the code is set, answer every
	// JSON-RPC request with this error.
	rpcErrCode    int
	rpcErrMessage string
	// dropOnRequest closes the WebSocket when the first request arrives.
	dropOnRequest bool
}

// serveEndpoint starts the fake endpoint and returns its address with scheme.
func serveEndpoint(t *testing.T, e endpoint, scheme string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e.handshakeStatus != 0 {
			w.WriteHeader(e.handshakeStatus)
			return
		}
		up := websocket.Upgrader{}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req struct {
				ID json.RawMessage `json:"id"`
			}
			if json.Unmarshal(msg, &req) != nil || req.ID == nil {
				continue
			}
			if e.dropOnRequest {
				return
			}
			reply := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":"0x1"}`, req.ID)
			if e.rpcErrCode != 0 {
				reply = fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":%d,"message":%q}}`, req.ID, e.rpcErrCode, e.rpcErrMessage)
			}
			if conn.WriteMessage(websocket.TextMessage, []byte(reply)) != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return scheme + strings.TrimPrefix(srv.URL, "http")
}

// callEth makes one eth_blockNumber call through go-ethereum, the way Piri's
// eth client does, and returns its error.
func callEth(t *testing.T, e endpoint, scheme string) error {
	t.Helper()
	ctx := context.Background()
	rc, err := rpc.DialOptions(ctx, serveEndpoint(t, e, scheme))
	if err != nil {
		return err
	}
	t.Cleanup(rc.Close)
	_, err = ethclient.NewClient(rc).BlockNumber(ctx)
	return err
}

// callLotus makes one Filecoin.ChainHead call through go-jsonrpc, the way
// Piri's Lotus client does, and returns its error.
func callLotus(t *testing.T, e endpoint) error {
	t.Helper()
	var api struct {
		ChainHead func(context.Context) (string, error)
	}
	closer, err := jsonrpc.NewMergeClient(context.Background(), serveEndpoint(t, e, "ws"), "Filecoin", []any{&api}, nil)
	if err != nil {
		return err
	}
	t.Cleanup(closer)
	_, err = api.ChainHead(context.Background())
	return err
}

func requireErr(t *testing.T, err error) error {
	t.Helper()
	require.Error(t, err)
	return err
}
