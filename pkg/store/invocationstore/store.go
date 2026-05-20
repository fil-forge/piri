package invocationstore

import (
	"bytes"
	"context"
	"fmt"

	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"

	"github.com/fil-forge/piri/pkg/store/genericstore"
	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/objectstore/dsadapter"
	"github.com/fil-forge/piri/pkg/store/objectstore/minio"
)

// InvocationStore stores UCAN invocations.
type InvocationStore interface {
	// Get retrieves a invocation by its root CID.
	Get(context.Context, cid.Cid) (ucan.Invocation, error)
	// Put adds or replaces a invocation in the store.
	Put(context.Context, ucan.Invocation) error
}

// KeyEncoder defines how to encode keys for a specific backend.
type KeyEncoder interface {
	EncodeKey(link cid.Cid) string
}

// Store implements InvocationStore backed by genericstore.
type Store struct {
	store   *genericstore.Store[ucan.Invocation]
	encoder KeyEncoder
}

var _ InvocationStore = (*Store)(nil)

// New creates a InvocationStore with the given backend and key encoder.
func New(backend objectstore.ListableStore, encoder KeyEncoder) *Store {
	return &Store{
		store:   genericstore.New[ucan.Invocation](backend, Codec{}),
		encoder: encoder,
	}
}

func (s *Store) Get(ctx context.Context, link cid.Cid) (ucan.Invocation, error) {
	dlg, err := s.store.Get(ctx, s.encoder.EncodeKey(link))
	if err != nil {
		return nil, fmt.Errorf("getting invocation: %w", err)
	}
	return dlg, nil
}

func (s *Store) Put(ctx context.Context, dlg ucan.Invocation) error {
	return s.store.Put(ctx, s.encoder.EncodeKey(dlg.Link()), dlg)
}

// Codec implements genericstore.Codec for ucan.Invocation
type Codec struct{}

func (Codec) Encode(inv ucan.Invocation) ([]byte, error) {
	return inv.Bytes(), nil
}

func (Codec) Decode(data []byte) (ucan.Invocation, error) {
	out := new(invocation.Invocation)
	if err := out.UnmarshalCBOR(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("decoding invocation: %w", err)
	}
	return out, nil
}

// S3KeyEncoder encodes keys for S3/MinIO backends.
type S3KeyEncoder struct{}

func (S3KeyEncoder) EncodeKey(link cid.Cid) string {
	return link.String()
}

// DatastoreKeyEncoder encodes keys for LevelDB/datastore backends.
type DatastoreKeyEncoder struct{}

func (DatastoreKeyEncoder) EncodeKey(link cid.Cid) string {
	return link.String()
}

// NewS3Store creates a InvocationStore for S3/MinIO backends.
// Delegations are stored with keys formatted as "invocations/{cid}".
func NewS3Store(backend *minio.Store) *Store {
	return New(backend, S3KeyEncoder{})
}

// NewDatastoreStore creates a InvocationStore for LevelDB/datastore backends.
// Delegations are stored with keys formatted as "{cid}".
func NewDatastoreStore(ds datastore.Datastore) *Store {
	return New(dsadapter.New(ds), DatastoreKeyEncoder{})
}
