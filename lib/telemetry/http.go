package telemetry

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewHTTPClient returns an HTTP client that records each request as a client
// span and propagates the trace context to the callee.
func NewHTTPClient() *http.Client {
	return &http.Client{Transport: NewTransport(http.DefaultTransport)}
}

// NewTransport wraps base so each request is a client span carrying the trace
// context to the callee.
func NewTransport(base http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(base)
}
