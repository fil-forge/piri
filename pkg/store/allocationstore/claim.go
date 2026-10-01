package allocationstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/fil-forge/ucantone/did"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

// PendingClaims is the slice of AllocationStore that ReleasePending needs.
type PendingClaims interface {
	GetPending(ctx context.Context, link cid.Cid) (allocation.Pending, error)
	DeletePending(ctx context.Context, link cid.Cid) error
	ListPending(ctx context.Context) iter.Seq2[allocation.Pending, error]
	Get(ctx context.Context, digest multihash.Multihash, space did.DID) (allocation.Allocation, error)
	Put(ctx context.Context, alloc allocation.Allocation) error
	Delete(ctx context.Context, digest multihash.Multihash, space did.DID) error
}

// ReleasePending deletes a pending allocation and, once its data was received,
// its claim on (digest, space). Another upload of the same content in the same
// space may share that claim, in which case the claim is handed to it rather
// than deleted; a claim this upload does not hold is left alone. The caller
// discards the upload itself first.
//
// The record is read again first: the upload may have recorded its digest
// since p was read, if it completed while the allocation was released. It
// returns the record it released, whose Digest is set if the data was
// received.
func ReleasePending(ctx context.Context, s PendingClaims, p allocation.Pending) (allocation.Pending, error) {
	latest, err := s.GetPending(ctx, p.Allocation)
	if err == nil {
		p = latest
	} else if !errors.Is(err, store.ErrNotFound) {
		return p, fmt.Errorf("getting pending allocation: %w", err)
	}
	if len(p.Digest) > 0 {
		alloc, err := s.Get(ctx, p.Digest, p.Space)
		switch {
		case errors.Is(err, store.ErrNotFound):
		case err != nil:
			return p, fmt.Errorf("getting allocation: %w", err)
		case alloc.Allocation == p.Allocation:
			if err := handOverClaim(ctx, s, p, alloc); err != nil {
				return p, err
			}
		}
	}
	if err := s.DeletePending(ctx, p.Allocation); err != nil {
		return p, fmt.Errorf("deleting pending allocation: %w", err)
	}
	return p, nil
}

// handOverClaim gives the (digest, space) allocation that p holds to another
// received upload of the same content in the same space, or deletes it when
// there is none.
func handOverClaim(ctx context.Context, s PendingClaims, p allocation.Pending, alloc allocation.Allocation) error {
	for other, err := range s.ListPending(ctx) {
		if err != nil {
			return fmt.Errorf("listing pending allocations: %w", err)
		}
		if other.Allocation == p.Allocation || other.Space != p.Space || !bytes.Equal(other.Digest, p.Digest) {
			continue
		}
		alloc.Cause = other.Cause
		alloc.Allocation = other.Allocation
		alloc.Expires = other.Expires
		if err := s.Put(ctx, alloc); err != nil {
			return fmt.Errorf("handing over allocation: %w", err)
		}
		return nil
	}
	if err := s.Delete(ctx, p.Digest, p.Space); err != nil {
		return fmt.Errorf("deleting allocation: %w", err)
	}
	return nil
}
