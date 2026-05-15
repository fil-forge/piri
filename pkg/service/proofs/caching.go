package proofs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	accesscaps "github.com/fil-forge/libforge/capabilities/access"
	"github.com/fil-forge/ucantone/client"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/execution"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/command"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/fil-forge/ucantone/ucan/invocation"
)

// ErrNoConnection signals that RequestAccess was called on a cache miss
// without a configured *ucantone/client.HTTPClient (via WithHTTPClnt or
// WithServiceURL).
var ErrNoConnection = errors.New("proofs: no HTTP connection configured for RequestAccess fetch")

// ErrDelegationNotGranted signals that the upstream service returned a
// successful /access/claim receipt but none of the returned delegations
// covered the requested command for the requested issuer.
var ErrDelegationNotGranted = errors.New("proofs: no delegation granted for the requested command")

// defaultMinTTL is the minimum time a cached delegation should still be valid
// for before it expires. If the TTL is less than this then consider it expired.
var defaultMinTTL = time.Second * 5

// CachingProofService is the in-memory cache for delegation proofs.
//
// On cache miss, RequestAccess invokes `/access/claim` against the
// configured upstream service. Returned delegations are inserted into the
// cache and the one matching the requested command is returned.
type CachingProofService struct {
	cache      map[did.DID]map[did.DID]map[command.Command]*delegation.Delegation
	cacheMutex sync.RWMutex
}

func NewCachingProofService() *CachingProofService {
	return &CachingProofService{
		cache: map[did.DID]map[did.DID]map[command.Command]*delegation.Delegation{},
	}
}

func (ps *CachingProofService) RequestAccess(
	ctx context.Context,
	issuer ucan.Signer,
	audience did.DID,
	cmd command.Command,
	cause *invocation.Invocation,
	options ...Option,
) (*delegation.Delegation, error) {
	cfg := requestConfig{}
	for _, opt := range options {
		opt(&cfg)
	}

	if d, ok := ps.lookup(issuer.DID(), audience, cmd, cfg.minTTL); ok {
		return d, nil
	}

	httpClnt, err := resolveHTTPClient(cfg)
	if err != nil {
		return nil, err
	}

	inv, err := accesscaps.Claim.Invoke(
		issuer,
		audience,
		&accesscaps.ClaimArguments{},
		invocation.WithAudience(audience),
	)
	if err != nil {
		return nil, fmt.Errorf("building /access/claim invocation: %w", err)
	}

	_ = cause // legacy /access/grant included a cause link; /access/claim has no equivalent.

	res, err := httpClnt.Execute(execution.NewRequest(ctx, inv))
	if err != nil {
		return nil, fmt.Errorf("executing /access/claim: %w", err)
	}

	meta := res.Metadata()
	if meta == nil {
		return nil, ErrDelegationNotGranted
	}

	// Populate the cache with every returned delegation, then return the
	// one matching the requested command.
	var match *delegation.Delegation
	for _, d := range meta.Delegations() {
		stored, decodeErr := delegation.Decode(d.Bytes())
		if decodeErr != nil {
			return nil, fmt.Errorf("decoding returned delegation: %w", decodeErr)
		}
		ps.Put(issuer.DID(), audience, stored.Command(), stored)
		if stored.Command() == cmd {
			match = stored
		}
	}

	if match == nil {
		return nil, ErrDelegationNotGranted
	}
	return match, nil
}

func resolveHTTPClient(cfg requestConfig) (*client.HTTPClient, error) {
	if cfg.httpClnt != nil {
		return cfg.httpClnt, nil
	}
	if cfg.url != nil {
		c, err := client.NewHTTP(cfg.url)
		if err != nil {
			return nil, fmt.Errorf("building HTTP client for %s: %w", cfg.url, err)
		}
		return c, nil
	}
	return nil, ErrNoConnection
}

func (ps *CachingProofService) lookup(issuer, audience did.DID, cmd command.Command, minTTLOpt *time.Duration) (*delegation.Delegation, bool) {
	ps.cacheMutex.RLock()
	defer ps.cacheMutex.RUnlock()

	issuerProofs, ok := ps.cache[issuer]
	if !ok {
		return nil, false
	}
	serviceProofs, ok := issuerProofs[audience]
	if !ok {
		return nil, false
	}
	d, ok := serviceProofs[cmd]
	if !ok {
		return nil, false
	}
	exp := d.Expiration()
	if exp == nil {
		return d, true
	}
	minTTL := defaultMinTTL
	if minTTLOpt != nil {
		minTTL = *minTTLOpt
	}
	if ucan.Now()+ucan.UnixTimestamp(minTTL.Seconds()) >= *exp {
		return nil, false
	}
	return d, true
}

// Put inserts a delegation into the cache. Useful for tests and any code path
// that obtains delegations out-of-band of RequestAccess.
func (ps *CachingProofService) Put(issuer, audience did.DID, cmd command.Command, d *delegation.Delegation) {
	ps.cacheMutex.Lock()
	defer ps.cacheMutex.Unlock()
	issuerProofs, ok := ps.cache[issuer]
	if !ok {
		issuerProofs = map[did.DID]map[command.Command]*delegation.Delegation{}
		ps.cache[issuer] = issuerProofs
	}
	serviceProofs, ok := issuerProofs[audience]
	if !ok {
		serviceProofs = map[command.Command]*delegation.Delegation{}
		issuerProofs[audience] = serviceProofs
	}
	serviceProofs[cmd] = d
}
