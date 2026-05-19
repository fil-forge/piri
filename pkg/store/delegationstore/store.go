package delegationstore

import (
	"bytes"
	"context"
	"fmt"

	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/delegation"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"

	"github.com/fil-forge/piri/pkg/store/genericstore"
	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/objectstore/dsadapter"
	"github.com/fil-forge/piri/pkg/store/objectstore/minio"
)

// DelegationStore stores UCAN delegations.
type DelegationStore interface {
	// Get retrieves a delegation by its root CID.
	Get(context.Context, cid.Cid) (ucan.Delegation, error)
	// Put adds or replaces a delegation in the store.
	Put(context.Context, ucan.Delegation) error
}

// KeyEncoder defines how to encode keys for a specific backend.
type KeyEncoder interface {
	EncodeKey(link cid.Cid) string
}

// Store implements DelegationStore backed by genericstore.
type Store struct {
	store   *genericstore.Store[ucan.Delegation]
	encoder KeyEncoder
}

var _ DelegationStore = (*Store)(nil)

// New creates a DelegationStore with the given backend and key encoder.
func New(backend objectstore.ListableStore, encoder KeyEncoder) *Store {
	return &Store{
		store:   genericstore.New[ucan.Delegation](backend, Codec{}),
		encoder: encoder,
	}
}

func (s *Store) Get(ctx context.Context, link cid.Cid) (ucan.Delegation, error) {
	dlg, err := s.store.Get(ctx, s.encoder.EncodeKey(link))
	if err != nil {
		return nil, fmt.Errorf("getting delegation: %w", err)
	}
	return dlg, nil
}

func (s *Store) Put(ctx context.Context, dlg ucan.Delegation) error {
	return s.store.Put(ctx, s.encoder.EncodeKey(dlg.Link()), dlg)
}

// Codec implements genericstore.Codec for delegation.Delegation.
type Codec struct{}

func (Codec) Encode(dlg ucan.Delegation) ([]byte, error) {
	return dlg.Bytes(), nil
}

func (Codec) Decode(data []byte) (ucan.Delegation, error) {
	out := new(delegation.Delegation)
	if err := out.UnmarshalCBOR(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("decoding delegation: %w", err)
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

// NewS3Store creates a DelegationStore for S3/MinIO backends.
// Delegations are stored with keys formatted as "delegations/{cid}".
func NewS3Store(backend *minio.Store) *Store {
	return New(backend, S3KeyEncoder{})
}

// NewDatastoreStore creates a DelegationStore for LevelDB/datastore backends.
// Delegations are stored with keys formatted as "{cid}".
func NewDatastoreStore(ds datastore.Datastore) *Store {
	return New(dsadapter.New(ds), DatastoreKeyEncoder{})
}
