package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/piri/pkg/store"
)

// UploadIndex records which upload holds the bytes of a blob that was received
// without its digest, until they are settled at the key of their digest.
type UploadIndex interface {
	// Upload returns the id of the upload holding digest's bytes, or false
	// when no upload does.
	Upload(ctx context.Context, digest multihash.Multihash) (string, bool, error)
	// Forget drops digest's entry. Forgetting a digest with none succeeds.
	Forget(ctx context.Context, digest multihash.Multihash) error
}

// WithUploads returns bs reading through uploads: a blob not at the key of its
// digest is read from the upload that holds it. Settle moves it there, and
// Delete removes it wherever it is.
func WithUploads(bs Blobstore, uploads UploadIndex) Blobstore {
	return &uploadStore{Blobstore: bs, uploads: uploads}
}

type uploadStore struct {
	Blobstore
	uploads UploadIndex
}

// Get tries the key of digest first: every blob that has been settled, and
// every blob received with its digest, is there. A blob that is not may be
// held by an upload, and one whose upload has just been settled is at its key
// again, so the key is tried once more before reporting it missing.
func (s *uploadStore) Get(ctx context.Context, digest multihash.Multihash, opts ...GetOption) (Object, error) {
	obj, err := s.Blobstore.Get(ctx, digest, opts...)
	if !errors.Is(err, store.ErrNotFound) {
		return obj, err
	}
	id, ok, err := s.uploads.Upload(ctx, digest)
	if err != nil {
		return nil, fmt.Errorf("looking up upload of blob: %w", err)
	}
	if !ok {
		return nil, store.ErrNotFound
	}
	obj, err = s.Blobstore.GetUpload(ctx, id, opts...)
	if !errors.Is(err, store.ErrNotFound) {
		return obj, err
	}
	return s.Blobstore.Get(ctx, digest, opts...)
}

// Delete removes the blob from the key of its digest and from the upload that
// holds it, if any. The upload's bytes go before its entry, so a failure in
// between leaves an entry for a blob being removed, never unreferenced bytes.
func (s *uploadStore) Delete(ctx context.Context, digest multihash.Multihash) error {
	if err := s.Blobstore.Delete(ctx, digest); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	id, ok, err := s.uploads.Upload(ctx, digest)
	if err != nil {
		return fmt.Errorf("looking up upload of blob: %w", err)
	}
	if !ok {
		return nil
	}
	if err := s.Blobstore.DeleteUpload(ctx, id); err != nil {
		return fmt.Errorf("deleting upload of blob: %w", err)
	}
	return s.uploads.Forget(ctx, digest)
}

// Settle writes the upload's bytes to the key of digest as read consumes them.
// The entry is dropped only once that write is complete, and the upload only
// after the entry, so a reader always finds the bytes at one key or the other.
// A settle interrupted after the write finds the blob at its key next time,
// and only finishes the cleanup.
func (s *uploadStore) Settle(ctx context.Context, digest multihash.Multihash, read func(r io.Reader, size int64) error) (bool, error) {
	id, ok, err := s.uploads.Upload(ctx, digest)
	if err != nil {
		return false, fmt.Errorf("looking up upload of blob: %w", err)
	}
	if !ok {
		return false, nil
	}
	if obj, err := s.Blobstore.Get(ctx, digest); err == nil {
		_ = obj.Body().Close()
		return false, s.forgetUpload(ctx, digest, id)
	} else if !errors.Is(err, store.ErrNotFound) {
		return false, err
	}

	obj, err := s.Blobstore.GetUpload(ctx, id)
	if err != nil {
		return false, fmt.Errorf("reading upload of blob: %w", err)
	}
	body := obj.Body()
	defer body.Close()
	size := obj.Size()

	pr, pw := io.Pipe()
	written := make(chan error, 1)
	go func() {
		err := s.Blobstore.Put(ctx, digest, uint64(size), pr)
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
	return true, s.forgetUpload(ctx, digest, id)
}

func (s *uploadStore) forgetUpload(ctx context.Context, digest multihash.Multihash, id string) error {
	if err := s.uploads.Forget(ctx, digest); err != nil {
		return fmt.Errorf("forgetting upload of blob: %w", err)
	}
	if err := s.Blobstore.DeleteUpload(ctx, id); err != nil {
		return fmt.Errorf("deleting settled upload: %w", err)
	}
	return nil
}
