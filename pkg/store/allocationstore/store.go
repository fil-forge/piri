package allocationstore

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/fil-forge/libforge/digestutil"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
	"github.com/fil-forge/piri/pkg/store/genericstore"
	"github.com/fil-forge/piri/pkg/store/keylock"
	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/objectstore/dsadapter"
	"github.com/fil-forge/piri/pkg/store/objectstore/minio"
)

// AllocationStore tracks the items that have been, or will soon be stored on
// the storage node.
type AllocationStore interface {
	// Get retrieves an allocation for a blob (digest) in a space (DID). It
	// returns [github.com/fil-forge/piri/pkg/store.ErrNotFound] if the allocation
	// does not exist.
	Get(context.Context, multihash.Multihash, did.DID) (allocation.Allocation, error)
	// GetAnyNonExpired retrieves any allocation for a blob that has not expired.
	// The now parameter should be the current unix timestamp in seconds.
	// Returns [github.com/fil-forge/piri/pkg/store.ErrNotFound] if no non-expired allocation exists.
	GetAnyNonExpired(ctx context.Context, digest multihash.Multihash, now ucan.UnixTimestamp) (allocation.Allocation, error)
	// Exists checks if any allocation exists for a blob (digest).
	Exists(context.Context, multihash.Multihash) (bool, error)
	// Put adds or replaces allocation data in the store.
	Put(context.Context, allocation.Allocation) error
	// Delete removes the allocation for a blob (digest) in a space.
	// Deleting a missing allocation succeeds (idempotent).
	Delete(context.Context, multihash.Multihash, did.DID) error
	// DeleteByTask removes the allocation the `/blob/allocate` task link made,
	// while it is still the space's allocation for its blob, and reports
	// whether it did. An allocation since replaced by a later one is left in
	// place.
	DeleteByTask(ctx context.Context, link cid.Cid) (bool, error)
	// Claim adds alloc unless the space already holds an allocation for the
	// blob, and reports whether it did.
	Claim(context.Context, allocation.Allocation) (bool, error)
	// MakeCurrent makes the `/blob/allocate` task link the space's allocation
	// for the blob, if a later allocation replaced it. A space with no
	// allocation for the blob is left alone.
	MakeCurrent(ctx context.Context, digest multihash.Multihash, space did.DID, link cid.Cid) error
	// ListSpaces returns the DID of every space holding an allocation for
	// the digest. An unknown digest yields an empty list.
	ListSpaces(context.Context, multihash.Multihash) ([]did.DID, error)
	// GetByTask retrieves the allocation the `/blob/allocate` task link
	// made, while it is still the space's allocation for its blob. It returns
	// [github.com/fil-forge/piri/pkg/store.ErrNotFound] once the allocation is
	// deleted or replaced by a later allocation of the same blob in the space.
	GetByTask(context.Context, cid.Cid) (allocation.Allocation, error)

	// PutPending adds or replaces an allocation made without a digest.
	PutPending(context.Context, allocation.Pending) error
	// GetPending retrieves the allocation made without a digest by the
	// `/blob/allocate` task link. It returns
	// [github.com/fil-forge/piri/pkg/store.ErrNotFound] if there is none.
	GetPending(context.Context, cid.Cid) (allocation.Pending, error)
	// DeletePending removes the allocation made without a digest by the
	// `/blob/allocate` task link. Deleting a missing one succeeds.
	DeletePending(context.Context, cid.Cid) error
	// ListPending iterates every allocation made without a digest.
	ListPending(context.Context) iter.Seq2[allocation.Pending, error]
	// ReleasePending deletes an allocation made without a digest and, once
	// its data was received, its claim on (digest, space), handing the claim
	// to another received upload of the same content in the space if there
	// is one. It returns the record it released.
	ReleasePending(context.Context, allocation.Pending) (allocation.Pending, error)
}

// KeyEncoder defines how to encode keys for a specific backend.
type KeyEncoder interface {
	EncodeKey(digest multihash.Multihash, space did.DID) string
	EncodeKeyPrefix(digest multihash.Multihash) string
}

// Store implements AllocationStore backed by any ListableStore. Every change to
// a (digest, space) allocation runs under that key's lock, so a change that
// reads the allocation first acts on the allocation it read. The lock is held
// in this process, which is the only one using the store.
type Store struct {
	locks keylock.Locks

	store *genericstore.Store[allocation.Allocation]
	// byTask indexes store by `/blob/allocate` task link: a copy of each
	// allocation, which locates the (digest, space) record.
	byTask  *genericstore.Store[allocation.Allocation]
	pending *genericstore.Store[allocation.Pending]
	encoder KeyEncoder
}

// pendingNamespace keys allocations made without a digest by their
// `/blob/allocate` task link. Allocation keys start with a digest, which never
// starts with this namespace, so digest-prefix scans never see them.
const pendingNamespace = "pending/"

// byTaskNamespace keys the allocation index by `/blob/allocate` task
// link. Like pendingNamespace, it never prefixes an allocation key.
const byTaskNamespace = "by-task/"

var _ AllocationStore = (*Store)(nil)

// New creates an AllocationStore with the given backend and key encoder.
func New(backend objectstore.ListableStore, encoder KeyEncoder) *Store {
	traced := objectstore.TracedListable("allocations", backend)
	return &Store{
		store:   genericstore.New(traced, allocation.Codec{}),
		byTask:  genericstore.New(traced, allocation.Codec{}, genericstore.WithNamespace(byTaskNamespace)),
		pending: genericstore.New(traced, allocation.PendingCodec{}, genericstore.WithNamespace(pendingNamespace)),
		encoder: encoder,
	}
}

func (s *Store) Get(ctx context.Context, digest multihash.Multihash, space did.DID) (allocation.Allocation, error) {
	alloc, err := s.store.Get(ctx, s.encoder.EncodeKey(digest, space))
	if err != nil {
		return allocation.Allocation{}, fmt.Errorf("getting allocation: %w", err)
	}
	return alloc, nil
}

func (s *Store) GetAnyNonExpired(ctx context.Context, digest multihash.Multihash, now ucan.UnixTimestamp) (allocation.Allocation,
	error) {
	alloc, err := s.store.GetAnyMatching(ctx, s.encoder.EncodeKeyPrefix(digest), func(a allocation.Allocation) bool {
		return a.Expires > now
	})
	if err != nil {
		return allocation.Allocation{}, fmt.Errorf("getting non-expired allocation: %w", err)
	}
	return alloc, nil
}

func (s *Store) Exists(ctx context.Context, digest multihash.Multihash) (bool, error) {
	return s.store.ExistsWithPrefix(ctx, s.encoder.EncodeKeyPrefix(digest))
}

func (s *Store) Put(ctx context.Context, alloc allocation.Allocation) error {
	key := s.encoder.EncodeKey(alloc.Blob.Digest, alloc.Space)
	defer s.locks.Lock(key)()
	return s.put(ctx, key, alloc)
}

func (s *Store) Claim(ctx context.Context, alloc allocation.Allocation) (bool, error) {
	key := s.encoder.EncodeKey(alloc.Blob.Digest, alloc.Space)
	defer s.locks.Lock(key)()
	if _, err := s.store.Get(ctx, key); err == nil {
		return false, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return false, fmt.Errorf("getting allocation: %w", err)
	}
	return true, s.put(ctx, key, alloc)
}

func (s *Store) MakeCurrent(ctx context.Context, digest multihash.Multihash, space did.DID, link cid.Cid) error {
	key := s.encoder.EncodeKey(digest, space)
	defer s.locks.Lock(key)()
	alloc, err := s.store.Get(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting allocation: %w", err)
	}
	if alloc.Allocation == link {
		return nil
	}
	alloc.Allocation = link
	return s.put(ctx, key, alloc)
}

// put writes the index entry before the record, so a failure in between leaves
// a stale entry, which GetByTask ignores, never a record it cannot find.
// A replaced allocation's entry is dropped. The caller holds key's lock.
func (s *Store) put(ctx context.Context, key string, alloc allocation.Allocation) error {
	prev, err := s.store.Get(ctx, key)
	switch {
	case err == nil:
		if prev.Allocation.Defined() && prev.Allocation != alloc.Allocation {
			if err := s.byTask.Delete(ctx, prev.Allocation.String()); err != nil {
				return fmt.Errorf("dropping replaced allocation from index: %w", err)
			}
		}
	case !errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("getting allocation: %w", err)
	}
	if err := s.byTask.Put(ctx, alloc.Allocation.String(), alloc); err != nil {
		return fmt.Errorf("indexing allocation: %w", err)
	}
	return s.store.Put(ctx, key, alloc)
}

func (s *Store) Delete(ctx context.Context, digest multihash.Multihash, space did.DID) error {
	key := s.encoder.EncodeKey(digest, space)
	defer s.locks.Lock(key)()
	alloc, err := s.store.Get(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting allocation: %w", err)
	}
	return s.delete(ctx, key, alloc)
}

func (s *Store) DeleteByTask(ctx context.Context, link cid.Cid) (bool, error) {
	// A replaced allocation's index entry is dropped, so a link with no
	// entry made no current allocation.
	indexed, err := s.byTask.Get(ctx, link.String())
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("getting allocation by allocate task: %w", err)
	}
	key := s.encoder.EncodeKey(indexed.Blob.Digest, indexed.Space)
	defer s.locks.Lock(key)()
	alloc, err := s.store.Get(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("getting allocation: %w", err)
	}
	if alloc.Allocation != link {
		return false, nil
	}
	return true, s.delete(ctx, key, alloc)
}

// delete removes the record before its index entry, so a failure in between
// leaves only a stale entry. The caller holds key's lock.
func (s *Store) delete(ctx context.Context, key string, alloc allocation.Allocation) error {
	if err := s.store.Delete(ctx, key); err != nil {
		return err
	}
	if !alloc.Allocation.Defined() {
		return nil
	}
	return s.byTask.Delete(ctx, alloc.Allocation.String())
}

func (s *Store) GetByTask(ctx context.Context, link cid.Cid) (allocation.Allocation, error) {
	indexed, err := s.byTask.Get(ctx, link.String())
	if err != nil {
		return allocation.Allocation{}, fmt.Errorf("getting allocation by allocate task: %w", err)
	}
	alloc, err := s.Get(ctx, indexed.Blob.Digest, indexed.Space)
	if err != nil {
		return allocation.Allocation{}, err
	}
	if alloc.Allocation != link {
		return allocation.Allocation{}, fmt.Errorf("allocation %s was replaced: %w", link, store.ErrNotFound)
	}
	return alloc, nil
}

func (s *Store) ListSpaces(ctx context.Context, digest multihash.Multihash) ([]did.DID, error) {
	var spaces []did.DID
	for alloc, err := range s.store.ListPrefix(ctx, s.encoder.EncodeKeyPrefix(digest)) {
		if err != nil {
			return nil, fmt.Errorf("listing allocations for %s: %w", digestutil.Format(digest), err)
		}
		spaces = append(spaces, alloc.Space)
	}
	return spaces, nil
}

func (s *Store) PutPending(ctx context.Context, p allocation.Pending) error {
	return s.pending.Put(ctx, p.Allocation.String(), p)
}

func (s *Store) GetPending(ctx context.Context, link cid.Cid) (allocation.Pending, error) {
	p, err := s.pending.Get(ctx, link.String())
	if err != nil {
		return allocation.Pending{}, fmt.Errorf("getting pending allocation: %w", err)
	}
	return p, nil
}

func (s *Store) DeletePending(ctx context.Context, link cid.Cid) error {
	return s.pending.Delete(ctx, link.String())
}

func (s *Store) ListPending(ctx context.Context) iter.Seq2[allocation.Pending, error] {
	return s.pending.ListPrefix(ctx, "")
}

// S3KeyEncoder encodes keys for S3/MinIO backends (keys end with .cbor).
type S3KeyEncoder struct{}

func (S3KeyEncoder) EncodeKey(digest multihash.Multihash, space did.DID) string {
	return fmt.Sprintf("%s/%s.cbor", digestutil.Format(digest), space.String())
}

func (S3KeyEncoder) EncodeKeyPrefix(digest multihash.Multihash) string {
	return fmt.Sprintf("%s/", digestutil.Format(digest))
}

// DatastoreKeyEncoder encodes keys for LevelDB/datastore backends (no suffix).
type DatastoreKeyEncoder struct{}

func (DatastoreKeyEncoder) EncodeKey(digest multihash.Multihash, space did.DID) string {
	return fmt.Sprintf("%s/%s", digestutil.Format(digest), space.String())
}

func (DatastoreKeyEncoder) EncodeKeyPrefix(digest multihash.Multihash) string {
	return fmt.Sprintf("%s/", digestutil.Format(digest))
}

// NewS3Store creates an AllocationStore for S3/MinIO backends.
// Allocations are stored with keys formatted as "allocations/{digest}/{space}.cbor".
func NewS3Store(backend *minio.Store) *Store {
	return New(backend, S3KeyEncoder{})
}

// NewDatastoreStore creates an AllocationStore for LevelDB/datastore backends.
// Allocations are stored with keys formatted as "{digest}/{space}".
func NewDatastoreStore(ds datastore.Datastore) *Store {
	return New(dsadapter.New(ds), DatastoreKeyEncoder{})
}
