package proofs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/fil-forge/libforge/commands/access"
	"github.com/fil-forge/ucantone/client"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"
)

// defaultMinTTL is the minimum time a cached delegation should still be valid
// for before it expires. If the TTL is less than this then consider it expired.
var defaultMinTTL = time.Second * 5

// cacheKey is (issuer DID, audience DID, command string).
type cacheKey struct {
	issuer   did.DID
	audience did.DID
	command  ucan.Command
}

type CachingProofService struct {
	cache      map[cacheKey]ucan.Delegation
	cacheMutex sync.RWMutex
}

func NewCachingProofService() *CachingProofService {
	return &CachingProofService{
		cache: map[cacheKey]ucan.Delegation{},
	}
}

// RequestAccess requests a delegation from the named service for the given
// command. A cached delegation may be returned if it's still valid.
func (ps *CachingProofService) RequestAccess(
	ctx context.Context,
	issuer ucan.Issuer,
	audience did.DID,
	command ucan.Command,
	cause ucan.Invocation,
	options ...Option,
) (ucan.Delegation, error) {
	cfg := requestConfig{}
	for _, opt := range options {
		opt(&cfg)
	}

	httpClient := cfg.client
	if httpClient == nil {
		serviceURL := cfg.url
		if serviceURL == nil {
			s := audience.String()
			if !strings.HasPrefix(s, "did:web:") {
				return nil, errors.New("non-did:web audience and no service URL provided")
			}
			u, err := url.Parse("https://" + strings.TrimPrefix(s, "did:web:"))
			if err != nil {
				return nil, err
			}
			serviceURL = u
		}
		var opts []client.HTTPOption
		if cfg.httpClient != nil {
			opts = append(opts, client.WithHTTPClient(cfg.httpClient))
		}
		c, err := client.NewHTTP(serviceURL, opts...)
		if err != nil {
			return nil, fmt.Errorf("creating ucantone HTTP client for %s: %w", audience, err)
		}
		httpClient = c
	}

	key := cacheKey{issuer: issuer.DID(), audience: audience, command: command}

	ps.cacheMutex.RLock()
	if d, ok := ps.cache[key]; ok {
		exp := d.Expiration()
		if exp == nil {
			ps.cacheMutex.RUnlock()
			return d, nil
		}
		minTTL := defaultMinTTL
		if cfg.minTTL != nil {
			minTTL = *cfg.minTTL
		}
		if ucan.Now()+ucan.UnixTimestamp(int64(minTTL.Seconds())) < *exp {
			ps.cacheMutex.RUnlock()
			return d, nil
		}
	}
	ps.cacheMutex.RUnlock()

	// Cache miss or stale — fetch a fresh delegation.
	d, err := requestDelegation(ctx, httpClient, issuer, audience, command, cause)
	if err != nil {
		return nil, fmt.Errorf("requesting %s access from %s: %w", command, audience, err)
	}

	ps.cacheMutex.Lock()
	ps.cache[key] = d
	ps.cacheMutex.Unlock()

	return d, nil
}

func requestDelegation(
	ctx context.Context,
	httpClient *client.HTTPClient,
	issuer ucan.Issuer,
	audience did.DID,
	command ucan.Command,
	cause ucan.Invocation,
) (ucan.Delegation, error) {
	args := &access.GrantArguments{
		Attenuations: []access.CapabilityRequest{{Command: command}},
	}
	if cause != nil {
		c := cause.Link()
		args.Cause = &c
	}
	// /access/grant is the bootstrap step of the access flow: the issuer has
	// no prior delegation to lean on, so the invocation must be self-issued
	// (subject == issuer) with an explicit audience pointing at the service.
	inv, err := access.Grant.Invoke(issuer, issuer.DID(), args, invocation.WithAudience(audience))
	if err != nil {
		return nil, fmt.Errorf("building %s invocation: %w", access.GrantCommand, err)
	}

	var reqOpts []execution.RequestOption
	if cause != nil {
		reqOpts = append(reqOpts, execution.WithInvocations(cause))
	}
	resp, err := httpClient.Execute(execution.NewRequest(ctx, inv, reqOpts...))
	if err != nil {
		return nil, fmt.Errorf("executing %s: %w", access.GrantCommand, err)
	}

	out := resp.Receipt().Out()
	okBytes, _ := out.Unpack()
	if !out.IsOK() {
		return nil, fmt.Errorf("%s receipt is a failure", access.GrantCommand)
	}

	var ok access.GrantOK
	if err := ok.UnmarshalCBOR(bytes.NewReader(okBytes)); err != nil {
		return nil, fmt.Errorf("decoding %s receipt: %w", access.GrantCommand, err)
	}
	if len(ok.Delegations) == 0 {
		return nil, fmt.Errorf("%s receipt contains no delegations", access.GrantCommand)
	}

	// The signed delegation envelopes ride in the response container metadata
	// (set by the server via res.SetMetadata(container.New(WithDelegations...))).
	meta := resp.Metadata()
	for _, d := range meta.Delegations() {
		if d.Link() == ok.Delegations[0] {
			return d, nil
		}
	}
	return nil, fmt.Errorf("%s response container did not include delegation %s", access.GrantCommand, ok.Delegations[0])
}
