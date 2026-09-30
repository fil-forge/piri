package blobstore

import (
	"context"
	"errors"
	"io"

	"github.com/ipfs/go-datastore"
	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/objectstore/dsadapter"
	"github.com/fil-forge/piri/pkg/store/objectstore/flatfs"
	minio_store "github.com/fil-forge/piri/pkg/store/objectstore/minio"
)

var _ Blobstore = (*Store)(nil)

// Store wraps an objectstore.Store with a KeyEncoder for S3/MinIO/flatfs backends.
type Store struct {
	backend objectstore.Store
	encoder KeyEncoder
}

// NewS3Store creates a Blobstore backed by an S3/MinIO object store.
func NewS3Store(backend *minio_store.Store) *Store {
	return &Store{
		backend: objectstore.Traced("blobs", backend),
		encoder: NewBase32FlatFSKeyEncoder(),
	}
}

// NewFlatfsStore creates a Blobstore backed by a flatfs object store.
func NewFlatfsStore(backend *flatfs.Store) *Store {
	return &Store{
		backend: objectstore.Traced("blobs", backend),
		encoder: Base32KeyEncoder{},
	}
}

// NewDatastoreStore creates a Blobstore backed by a datastore.Datastore.
// Useful for testing with sync.MutexWrap(datastore.NewMapDatastore()).
func NewDatastoreStore(ds datastore.Datastore) *Store {
	return &Store{
		backend: objectstore.Traced("blobs", dsadapter.New(ds)),
		encoder: PlainKeyEncoder{},
	}
}

func (s *Store) Get(ctx context.Context, digest multihash.Multihash, opts ...GetOption) (Object, error) {
	return s.get(ctx, s.encoder.EncodeKey(digest), opts...)
}

func (s *Store) get(ctx context.Context, key string, opts ...GetOption) (Object, error) {
	o := &GetOptions{}
	for _, opt := range opts {
		opt(o)
	}
	obj, err := s.backend.Get(ctx, key, objectstore.WithRange(objectstore.Range(o.ByteRange)))
	if err != nil {
		if errors.Is(err, objectstore.ErrNotExist) {
			return nil, store.ErrNotFound
		}
		var erns objectstore.ErrRangeNotSatisfiable
		if errors.As(err, &erns) {
			return nil, NewRangeNotSatisfiableError(Range{Start: erns.Range.Start, End: erns.Range.End})
		}
		return nil, err
	}
	return obj, nil
}

func (s *Store) Put(ctx context.Context, digest multihash.Multihash, size uint64, body io.Reader) error {
	return s.backend.Put(ctx, s.encoder.EncodeKey(digest), size, body)
}

func (s *Store) Delete(ctx context.Context, digest multihash.Multihash) error {
	return s.backend.Delete(ctx, s.encoder.EncodeKey(digest))
}

// uploadKey is the key of an upload. It is valid in every backend and cannot
// collide with a digest key, which never starts with "upload-".
func uploadKey(id string) string {
	return "upload-" + id
}

func (s *Store) PutUpload(ctx context.Context, id string, size uint64, body io.Reader) error {
	return s.backend.Put(ctx, uploadKey(id), size, body)
}

func (s *Store) GetUpload(ctx context.Context, id string, opts ...GetOption) (Object, error) {
	return s.get(ctx, uploadKey(id), opts...)
}

func (s *Store) DeleteUpload(ctx context.Context, id string) error {
	err := s.backend.Delete(ctx, uploadKey(id))
	if errors.Is(err, objectstore.ErrNotExist) {
		return nil
	}
	return err
}

// Settle reports false: without an [UploadIndex] no upload is known to hold a
// blob. [WithUploads] provides one.
func (s *Store) Settle(context.Context, multihash.Multihash, func(io.Reader, int64) error) (bool, error) {
	return false, nil
}
