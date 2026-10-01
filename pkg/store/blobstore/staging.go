package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/objectstore"
)

// StagingIndex records which stage ID holds the bytes of a blob that was
// received without its digest, until they are settled at the key of their
// digest.
type StagingIndex interface {
	// GetID returns the ID of the staged blob whose bytes are digest's, or false
	// when none is.
	GetID(ctx context.Context, digest multihash.Multihash) (string, bool, error)
	// Delete drops digest's entry. Deleting a digest with no entry succeeds.
	Delete(ctx context.Context, digest multihash.Multihash) error
}

// StagingStore stages blobs whose digest is not yet known, and is a
// Blobstore reading through them: a blob not at the key of its digest is read
// from where it is staged, as the index records. Settle moves it to its key,
// and Delete removes it wherever it is. Every reader of blobs that may have
// arrived without their digest reads through the same StagingStore.
//
// Settle and Delete of the same digest run one at a time. A Delete during a
// Settle would otherwise unstage the blob and drop its entry while the Settle
// is still reading it, and the Settle would then write a copy of the removed blob
// to its key that nothing refers to. The lock is held in this process, which
// is the only one using the store.
type StagingStore struct {
	blobs   Blobstore
	staging objectstore.Store
	index   StagingIndex

	mu    sync.Mutex
	locks map[string]*digestLock
}

var _ Blobstore = (*StagingStore)(nil)

// NewStagingStore returns a blob store that can store blobs in a staging area
// before moving them to their final location in the blob store.
func NewStagingStore(blobs Blobstore, staging objectstore.Store, index StagingIndex) *StagingStore {
	return &StagingStore{
		blobs:   blobs,
		staging: objectstore.Traced("staging", staging),
		index:   index,
		locks:   map[string]*digestLock{},
	}
}

// stagedKey is the key of a staged blob. No blob key starts with "staged-":
// blob keys are base32 or base58 encodings of the digest, which contain no "-".
func stagedKey(id string) string {
	return "staged-" + id
}

// Stage stages a blob whose digest is not yet known under id, typically the ID
// of an upload. Its bytes stay staged once their digest is known, until Settle
// moves them.
func (s *StagingStore) Stage(ctx context.Context, id string, size uint64, body io.Reader) error {
	return s.staging.Put(ctx, stagedKey(id), size, body)
}

// GetStaged retrieves the staged blob by ID. Returns [store.ErrNotFound] if it
// is not staged.
func (s *StagingStore) GetStaged(ctx context.Context, id string, opts ...GetOption) (Object, error) {
	return getObject(ctx, s.staging, stagedKey(id), opts...)
}

// Unstage removes the staged blob by ID. Unstaging a blob that is not staged
// succeeds.
//
// It does not touch the index, which is keyed by digest: the caller must know
// that no entry names the ID, because an entry naming it would be left
// pointing at bytes that are gone, and Get would then report the blob missing.
// That holds for a blob whose digest was never computed, one that lost to
// another upload of the same content, and one whose discard found no entry
// naming it. A blob the index records is removed with Delete, entry and all.
func (s *StagingStore) Unstage(ctx context.Context, id string) error {
	err := s.staging.Delete(ctx, stagedKey(id))
	if errors.Is(err, objectstore.ErrNotExist) {
		return nil
	}
	return err
}

// Put stores a blob whose digest is known.
func (s *StagingStore) Put(ctx context.Context, digest multihash.Multihash, size uint64, body io.Reader) error {
	return s.blobs.Put(ctx, digest, size, body)
}

type digestLock struct {
	sync.Mutex
	// waiters counts the holder and everyone waiting; the lock is dropped
	// from the map when it reaches zero.
	waiters int
}

// lock serializes Settle and Delete of digest, and returns the unlock.
func (s *StagingStore) lock(digest multihash.Multihash) func() {
	key := string(digest)
	s.mu.Lock()
	l, ok := s.locks[key]
	if !ok {
		l = &digestLock{}
		s.locks[key] = l
	}
	l.waiters++
	s.mu.Unlock()

	l.Lock()
	return func() {
		l.Unlock()
		s.mu.Lock()
		l.waiters--
		if l.waiters == 0 {
			delete(s.locks, key)
		}
		s.mu.Unlock()
	}
}

// Get tries the key of digest first: every blob that has been settled, and
// every blob received with its digest, is there. A blob that is not may be
// staged, and one that has just been settled is at its key again, so the key
// is tried once more before reporting it missing.
func (s *StagingStore) Get(ctx context.Context, digest multihash.Multihash, opts ...GetOption) (Object, error) {
	obj, err := s.blobs.Get(ctx, digest, opts...)
	if !errors.Is(err, store.ErrNotFound) {
		return obj, err
	}
	id, ok, err := s.index.GetID(ctx, digest)
	if err != nil {
		return nil, fmt.Errorf("looking up staged blob: %w", err)
	}
	if !ok {
		return nil, store.ErrNotFound
	}
	obj, err = s.GetStaged(ctx, id, opts...)
	if !errors.Is(err, store.ErrNotFound) {
		return obj, err
	}
	return s.blobs.Get(ctx, digest, opts...)
}

// Delete removes the blob from the key of its digest and from staging, if it
// is staged. The staged bytes go before their entry, so a failure in between
// leaves an entry for a blob being removed, never unreferenced bytes.
func (s *StagingStore) Delete(ctx context.Context, digest multihash.Multihash) error {
	defer s.lock(digest)()
	if err := s.blobs.Delete(ctx, digest); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	id, ok, err := s.index.GetID(ctx, digest)
	if err != nil {
		return fmt.Errorf("looking up staged blob: %w", err)
	}
	if !ok {
		return nil
	}
	if err := s.Unstage(ctx, id); err != nil {
		return fmt.Errorf("unstaging blob: %w", err)
	}
	return s.index.Delete(ctx, digest)
}

// Settle moves the staged bytes of digest to the key of digest, handing them to
// read on the way so the move costs no read of its own. read gets the blob's
// bytes and size; the move completes once read returns without error. It
// reports false, without calling read, when the blob is not staged.
//
// Settle writes the staged bytes to the key of digest as read consumes them.
// Once that write is complete the blob is unstaged, and its entry dropped
// after, so the entry stays until the cleanup it records is done. A settle
// interrupted after the write finds the entry and the blob at its key next
// time, and only finishes the cleanup. A reader that finds the entry after the
// blob is unstaged reads it from its key, as Get does.
func (s *StagingStore) Settle(ctx context.Context, digest multihash.Multihash, read func(r io.Reader, size int64) error) (bool, error) {
	defer s.lock(digest)()
	id, ok, err := s.index.GetID(ctx, digest)
	if err != nil {
		return false, fmt.Errorf("looking up staged blob: %w", err)
	}
	if !ok {
		return false, nil
	}
	if obj, err := s.blobs.Get(ctx, digest); err == nil {
		_ = obj.Body().Close()
		return false, s.unstageSettled(ctx, digest, id)
	} else if !errors.Is(err, store.ErrNotFound) {
		return false, err
	}

	obj, err := s.GetStaged(ctx, id)
	if err != nil {
		return false, fmt.Errorf("reading staged blob: %w", err)
	}
	body := obj.Body()
	defer body.Close()
	size := obj.Size()

	pr, pw := io.Pipe()
	written := make(chan error, 1)
	go func() {
		err := s.blobs.Put(ctx, digest, uint64(size), pr)
		pr.CloseWithError(err)
		written <- err
	}()
	tee := io.TeeReader(body, pw)
	err = read(tee, size)
	if err == nil {
		// Whatever read left unread still has to reach the new key.
		_, err = io.Copy(io.Discard, tee)
	}
	if err != nil {
		pw.CloseWithError(err)
		<-written
		return true, err
	}
	pw.Close()
	if err := <-written; err != nil {
		return true, fmt.Errorf("writing blob to its digest: %w", err)
	}
	return true, s.unstageSettled(ctx, digest, id)
}

func (s *StagingStore) unstageSettled(ctx context.Context, digest multihash.Multihash, id string) error {
	if err := s.Unstage(ctx, id); err != nil {
		return fmt.Errorf("unstaging settled blob: %w", err)
	}
	if err := s.index.Delete(ctx, digest); err != nil {
		return fmt.Errorf("deleting staged blob's entry: %w", err)
	}
	return nil
}
