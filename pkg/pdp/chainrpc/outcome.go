package chainrpc

import (
	"context"
	"errors"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/rpc"
	jsonrpc "github.com/filecoin-project/go-jsonrpc"
	"github.com/gorilla/websocket"
)

// outcome is the outcome attribute of a recorded call. The set is fixed so the
// attribute cannot grow with error text.
type outcome string

const (
	outcomeOK outcome = "ok"
	// outcomeUnauthorized: the endpoint refused the key (HTTP 401 or 403).
	outcomeUnauthorized outcome = "unauthorized"
	// outcomeRateLimited: the endpoint refused the call as over a limit, by
	// HTTP 429, JSON-RPC error -32005, or an error that says so.
	outcomeRateLimited outcome = "rate_limited"
	// outcomeUnavailable: no answer from the endpoint, a connection that
	// failed or dropped, or an HTTP 5xx.
	outcomeUnavailable outcome = "unavailable"
	outcomeTimeout     outcome = "timeout"
	// outcomeCanceled: the caller gave up; it says nothing about the endpoint.
	outcomeCanceled outcome = "canceled"
	// outcomeError: the endpoint answered with an error about the call itself,
	// such as a reverted call or a missing actor.
	outcomeError outcome = "error"
)

// codeLimitExceeded is JSON-RPC's "Limit exceeded" error (EIP-1474).
const codeLimitExceeded = -32005

var (
	// go-ethereum writes a refused WebSocket handshake's status into the
	// error text only, and Piri's Lotus dial error copies that form.
	httpStatusPattern = regexp.MustCompile(`HTTP status (\d{3})`)
	// go-jsonrpc reports an error response as "RPC error (<code>): <message>".
	rpcCodePattern   = regexp.MustCompile(`RPC error \((-?\d+)\)`)
	rateLimitPattern = regexp.MustCompile(`(?i)rate.?limit|too many requests`)
)

// classifyOutcome sorts a chain RPC call's error into an outcome. The checks
// run from the most specific evidence (an HTTP status) to the least (words in
// the error text).
func classifyOutcome(err error) outcome {
	switch {
	case err == nil:
		return outcomeOK
	case errors.Is(err, context.Canceled):
		return outcomeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return outcomeTimeout
	}
	if status, ok := getHTTPStatus(err); ok {
		return classifyHTTPStatus(status)
	}
	if isConnectionError(err) {
		return outcomeUnavailable
	}
	if code, ok := getRPCErrorCode(err); ok && code == codeLimitExceeded {
		return outcomeRateLimited
	}
	if rateLimitPattern.MatchString(err.Error()) {
		return outcomeRateLimited
	}
	return outcomeError
}

func classifyHTTPStatus(status int) outcome {
	switch {
	case status == 401 || status == 403:
		return outcomeUnauthorized
	case status == 429:
		return outcomeRateLimited
	case status >= 500:
		return outcomeUnavailable
	default:
		return outcomeError
	}
}

// getHTTPStatus returns the HTTP status an HTTP call or a WebSocket handshake
// was refused with.
func getHTTPStatus(err error) (int, bool) {
	var httpErr rpc.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode, true
	}
	if m := httpStatusPattern.FindStringSubmatch(err.Error()); m != nil {
		status, convErr := strconv.Atoi(m[1])
		return status, convErr == nil
	}
	return 0, false
}

// getRPCErrorCode returns the code of a JSON-RPC error response.
func getRPCErrorCode(err error) (int, bool) {
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) {
		return rpcErr.ErrorCode(), true
	}
	if m := rpcCodePattern.FindStringSubmatch(err.Error()); m != nil {
		code, convErr := strconv.Atoi(m[1])
		return code, convErr == nil
	}
	return 0, false
}

// isConnectionError reports a call that failed because the connection did.
// go-jsonrpc fails a call in flight on a dropped socket with a JSON-RPC error
// response of its own making, so its text is checked before any code is read.
func isConnectionError(err error) bool {
	var connErr *jsonrpc.RPCConnectionError
	var closeErr *websocket.CloseError
	var netErr net.Error
	return errors.As(err, &connErr) ||
		errors.As(err, &closeErr) ||
		errors.As(err, &netErr) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		strings.Contains(err.Error(), "websocket connection closed")
}
