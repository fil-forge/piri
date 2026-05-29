package proofs

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/fil-forge/ucantone/client"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan"
)

// ProofService requests proofs from other UCAN enabled nodes by making
// `/access/grant` invocations.
type ProofService interface {
	// RequestAccess requests a delegation grant from the service identified
	// by `audience` for the named `command`. The optional `cause` invocation,
	// when non-nil, is attached to the access/grant request as supporting
	// context.
	RequestAccess(
		ctx context.Context,
		issuer ucan.Signer,
		audience did.DID,
		command ucan.Command,
		cause ucan.Invocation,
		options ...Option,
	) (ucan.Delegation, error)
}

type requestConfig struct {
	httpClient *http.Client
	url        *url.URL
	client     *client.HTTPClient
	minTTL     *time.Duration
}

type Option func(*requestConfig)

// WithHTTPClient configures a HTTP client to use in the request.
func WithHTTPClient(h *http.Client) Option {
	return func(cfg *requestConfig) {
		cfg.httpClient = h
	}
}

// WithServiceURL configures the URL of the service to request from. If not
// set it is inferred from the service DID, if it is a did:web.
func WithServiceURL(u *url.URL) Option {
	return func(cfg *requestConfig) {
		cfg.url = u
	}
}

// WithClient configures a pre-built ucantone HTTP client to use for the
// request. If set, the HTTP client and service URL options are ignored.
func WithClient(c *client.HTTPClient) Option {
	return func(cfg *requestConfig) {
		cfg.client = c
	}
}

// WithMinimumTTL configures the minimum TTL a cached delegation should have.
func WithMinimumTTL(minTTL time.Duration) Option {
	return func(cfg *requestConfig) {
		cfg.minTTL = &minTTL
	}
}
