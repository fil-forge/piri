package proofs

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/fil-forge/ucantone/client"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/command"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
)

// ProofService requests delegation proofs from a remote UCAN service.
//
// In UCAN 0.x this was a synchronous `/access/grant` invocation that returned
// the requested delegation inline. The libforge equivalents (`access.Request`,
// `access.Confirm`, `access.Claim`) model a two-step request/confirm flow. The
// Phase 4 client migration has not yet rewritten this call path; the current
// CachingProofService returns ErrNotMigrated.
type ProofService interface {
	// RequestAccess fetches a delegation granting `issuer` permission to
	// invoke `command` on `audience`. Cause is the invocation that motivates
	// the access request (typically the invocation that needs the delegation
	// as a proof).
	RequestAccess(
		ctx context.Context,
		issuer ucan.Signer,
		audience did.DID,
		command command.Command,
		cause *invocation.Invocation,
		options ...Option,
	) (*delegation.Delegation, error)
}

type requestConfig struct {
	httpClient *http.Client
	url        *url.URL
	httpClnt   *client.HTTPClient
	minTTL     *time.Duration
}

type Option func(*requestConfig)

// WithHTTPClient configures a HTTP client to use in the request.
func WithHTTPClient(h *http.Client) Option {
	return func(cfg *requestConfig) {
		cfg.httpClient = h
	}
}

// WithServiceURL configures the URL of the service to request from. If not set
// it will be inferred from the service DID, if it is a did:web.
func WithServiceURL(url *url.URL) Option {
	return func(cfg *requestConfig) {
		cfg.url = url
	}
}

// WithHTTPClnt overrides the underlying ucantone client used to issue the
// request. If set, the HTTPClient and ServiceURL options are ignored.
func WithHTTPClnt(c *client.HTTPClient) Option {
	return func(cfg *requestConfig) {
		cfg.httpClnt = c
	}
}

// WithMinimumTTL configures the minimum TTL a cached delegation should have.
func WithMinimumTTL(minTTL time.Duration) Option {
	return func(cfg *requestConfig) {
		cfg.minTTL = &minTTL
	}
}
