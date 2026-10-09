package allocationstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ipfs/go-cid"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

// ReleasePending re-reads the record first: the upload may have recorded its
// digest since p was read, if it completed while the allocation was released.
// Once the data was received, the claim, its handover and the deletion of the
// record all run under the claim's lock, so a concurrent release of another
// upload of the same content sees this record gone and the claim's holder as
// it now is.
func (s *Store) ReleasePending(ctx context.Context, p allocation.Pending) (allocation.Pending, error) {
	latest, err := s.GetPending(ctx, p.Allocation)
	if err == nil {
		p = latest
	} else if !errors.Is(err, store.ErrNotFound) {
		return p, fmt.Errorf("getting pending allocation: %w", err)
	}
	if len(p.Digest) > 0 {
		key := s.encoder.EncodeKey(p.Digest, p.Space)
		defer s.locks.Lock(key)()
		alloc, err := s.store.Get(ctx, key)
		switch {
		case errors.Is(err, store.ErrNotFound):
		case err != nil:
			return p, fmt.Errorf("getting allocation: %w", err)
		case alloc.Allocation == p.Allocation:
			if err := s.handOverClaim(ctx, key, p.Allocation, alloc); err != nil {
				return p, err
			}
		}
	}
	if err := s.DeletePending(ctx, p.Allocation); err != nil {
		return p, fmt.Errorf("deleting pending allocation: %w", err)
	}
	return p, nil
}

// handOverClaim gives the (digest, space) allocation that the task link
// released held to another received upload of the same content in the same
// space, or deletes it when there is none. The caller holds key's lock.
func (s *Store) handOverClaim(ctx context.Context, key string, released cid.Cid, alloc allocation.Allocation) error {
	for other, err := range s.ListPending(ctx) {
		if err != nil {
			return fmt.Errorf("listing pending allocations: %w", err)
		}
		if other.Allocation == released || other.Space != alloc.Space || !bytes.Equal(other.Digest, alloc.Blob.Digest) {
			continue
		}
		alloc.Cause = other.Cause
		alloc.Allocation = other.Allocation
		alloc.Expires = other.Expires
		if err := s.put(ctx, key, alloc); err != nil {
			return fmt.Errorf("handing over allocation: %w", err)
		}
		return nil
	}
	if err := s.delete(ctx, key, alloc); err != nil {
		return fmt.Errorf("deleting allocation: %w", err)
	}
	return nil
}
