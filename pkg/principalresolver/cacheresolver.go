package principalresolver

import (
	"context"
	"time"

	"github.com/fil-forge/ucantone/did"
	"github.com/patrickmn/go-cache"
)

// Resolver is the underlying resolver shape: same signature as
// [validator.DIDResolverFunc] but exposed as an interface so it can be
// wrapped (e.g. by [CachedResolver]).
type Resolver interface {
	Resolve(ctx context.Context, input did.DID) ([]did.DID, error)
}

type CachedResolver struct {
	wrapped Resolver
	cache   *cache.Cache
}

func NewCachedResolver(wrapped Resolver, ttl time.Duration) (*CachedResolver, error) {
	// items remain in the cache for `ttl`, expired items are purged every hour.
	return &CachedResolver{wrapped: wrapped, cache: cache.New(ttl, time.Hour)}, nil
}

func (c *CachedResolver) Resolve(ctx context.Context, input did.DID) ([]did.DID, error) {
	if out, found := c.cache.Get(input.String()); found {
		return out.([]did.DID), nil
	}
	out, err := c.wrapped.Resolve(ctx, input)
	if err != nil {
		return nil, err
	}
	c.cache.Set(input.String(), out, cache.DefaultExpiration)
	return out, nil
}
