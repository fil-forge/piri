package acceptancestore

import (
	"context"
	"errors"
	"fmt"

	"github.com/fil-forge/ucantone/did"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
)

// Getter is the slice of AcceptanceStore that AcceptedAllocation needs.
type Getter interface {
	Get(ctx context.Context, digest multihash.Multihash, space did.DID) (acceptance.Acceptance, error)
}

// AcceptedAllocation reports whether the space's acceptance of digest accepted
// the allocation link names. The acceptance is written before anything else
// records the accept, so it is the record a reject or an expiry consults. A
// digest that is not known yet, because the data was never received, has no
// acceptance.
func AcceptedAllocation(ctx context.Context, s Getter, digest multihash.Multihash, space did.DID, link cid.Cid) (bool, error) {
	if len(digest) == 0 {
		return false, nil
	}
	acc, err := s.Get(ctx, digest, space)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("getting acceptance: %w", err)
	}
	return acc.Allocation != nil && *acc.Allocation == link, nil
}
