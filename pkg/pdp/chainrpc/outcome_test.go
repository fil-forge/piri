package chainrpc

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyOutcome(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  func(t *testing.T) error
		want outcome
	}{
		{"a call that succeeded", func(*testing.T) error { return nil }, outcomeOK},
		{"a call its caller cancelled", func(*testing.T) error { return fmt.Errorf("reading head: %w", context.Canceled) }, outcomeCanceled},
		{"a call past its deadline", func(*testing.T) error { return fmt.Errorf("reading head: %w", context.DeadlineExceeded) }, outcomeTimeout},
		{"an eth WebSocket handshake refused with 401", func(t *testing.T) error {
			return requireErr(t, callEth(t, endpoint{handshakeStatus: http.StatusUnauthorized}, "ws"))
		}, outcomeUnauthorized},
		{"an eth WebSocket handshake refused with 403", func(t *testing.T) error {
			return requireErr(t, callEth(t, endpoint{handshakeStatus: http.StatusForbidden}, "ws"))
		}, outcomeUnauthorized},
		{"an eth WebSocket handshake refused with 429", func(t *testing.T) error {
			return requireErr(t, callEth(t, endpoint{handshakeStatus: http.StatusTooManyRequests}, "ws"))
		}, outcomeRateLimited},
		{"an eth WebSocket handshake refused with 503", func(t *testing.T) error {
			return requireErr(t, callEth(t, endpoint{handshakeStatus: http.StatusServiceUnavailable}, "ws"))
		}, outcomeUnavailable},
		{"an eth HTTP call refused with 401", func(t *testing.T) error {
			return requireErr(t, callEth(t, endpoint{handshakeStatus: http.StatusUnauthorized}, "http"))
		}, outcomeUnauthorized},
		{"an eth HTTP call refused with 429", func(t *testing.T) error {
			return requireErr(t, callEth(t, endpoint{handshakeStatus: http.StatusTooManyRequests}, "http"))
		}, outcomeRateLimited},
		{"an eth JSON-RPC limit error (-32005)", func(t *testing.T) error {
			return requireErr(t, callEth(t, endpoint{rpcErrCode: -32005, rpcErrMessage: "limit exceeded"}, "ws"))
		}, outcomeRateLimited},
		{"an eth JSON-RPC error that names a rate limit", func(t *testing.T) error {
			return requireErr(t, callEth(t, endpoint{rpcErrCode: -32000, rpcErrMessage: "rate limit reached"}, "ws"))
		}, outcomeRateLimited},
		{"an eth JSON-RPC error about the call itself", func(t *testing.T) error {
			return requireErr(t, callEth(t, endpoint{rpcErrCode: -32000, rpcErrMessage: "execution reverted"}, "ws"))
		}, outcomeError},
		{"an eth WebSocket dropped mid-call", func(t *testing.T) error { return requireErr(t, callEth(t, endpoint{dropOnRequest: true}, "ws")) }, outcomeUnavailable},
		{"a Lotus JSON-RPC limit error (-32005)", func(t *testing.T) error {
			return requireErr(t, callLotus(t, endpoint{rpcErrCode: -32005, rpcErrMessage: "limit exceeded"}))
		}, outcomeRateLimited},
		{"a Lotus JSON-RPC error about the call itself", func(t *testing.T) error {
			return requireErr(t, callLotus(t, endpoint{rpcErrCode: -32603, rpcErrMessage: "actor not found"}))
		}, outcomeError},
		{"a Lotus WebSocket dropped mid-call", func(t *testing.T) error { return requireErr(t, callLotus(t, endpoint{dropOnRequest: true})) }, outcomeUnavailable},
	} {
		t.Run("classifies "+tc.name+" as "+string(tc.want), func(t *testing.T) {
			err := tc.err(t)
			require.Equal(t, tc.want, classifyOutcome(err), "error: %v", err)
		})
	}
}
