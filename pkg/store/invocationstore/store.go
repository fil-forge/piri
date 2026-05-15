package invocationstore

import (
	"context"
	"fmt"

	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"

	"github.com/fil-forge/piri/pkg/store/genericstore"
	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/objectstore/dsadapter"
	"github.com/fil-forge/piri/pkg/store/objectstore/minio"
)

// InvocationStore stores UCAN invocations. In UCAN 1.0, content claims (e.g.
// /assert/location) are modeled as signed invocations rather than delegations,
// so this store is the natural backing for claim storage and similar uses.
type InvocationStore interface {
	// Get retrieves an invocation by its root CID.
	Get(context.Context, cid.Cid) (*invocation.Invocation, error)
	// Put adds or replaces an invocation in the store.
	Put(context.Context, *invocation.Invocation) error
}

// KeyEncoder defines how to encode keys for a specific backend.
type KeyEncoder interface {
	EncodeKey(link cid.Cid) string
}

// Store implements InvocationStore backed by genericstore.
type Store struct {
	store   *genericstore.Store[*invocation.Invocation]
	encoder KeyEncoder
}

var _ InvocationStore = (*Store)(nil)

// New creates an InvocationStore with the given backend and key encoder.
func New(backend objectstore.ListableStore, encoder KeyEncoder) *Store {
	return &Store{
		store:   genericstore.New[*invocation.Invocation](backend, Codec{}),
		encoder: encoder,
	}
}

func (s *Store) Get(ctx context.Context, link cid.Cid) (*invocation.Invocation, error) {
	inv, err := s.store.Get(ctx, s.encoder.EncodeKey(link))
	if err != nil {
		return nil, fmt.Errorf("getting invocation: %w", err)
	}
	return inv, nil
}

func (s *Store) Put(ctx context.Context, inv *invocation.Invocation) error {
	return s.store.Put(ctx, s.encoder.EncodeKey(inv.Link()), inv)
}

// Codec implements genericstore.Codec for *invocation.Invocation. Stored value
// is the raw dag-cbor envelope bytes.
type Codec struct{}

func (Codec) Encode(inv *invocation.Invocation) ([]byte, error) {
	return inv.Bytes(), nil
}

func (Codec) Decode(data []byte) (*invocation.Invocation, error) {
	return invocation.Decode(data)
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

// NewS3Store creates an InvocationStore for S3/MinIO backends.
func NewS3Store(backend *minio.Store) *Store {
	return New(backend, S3KeyEncoder{})
}

// NewDatastoreStore creates an InvocationStore for LevelDB/datastore backends.
func NewDatastoreStore(ds datastore.Datastore) *Store {
	return New(dsadapter.New(ds), DatastoreKeyEncoder{})
}
