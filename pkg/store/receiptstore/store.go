package receiptstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"

	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/receipt"

	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/genericstore"
	"github.com/fil-forge/piri/pkg/store/objectstore"
	"github.com/fil-forge/piri/pkg/store/objectstore/dsadapter"
	"github.com/fil-forge/piri/pkg/store/objectstore/minio"
)

// ReceiptStore stores UCAN invocation receipts.
type ReceiptStore interface {
	// Get retrieves a receipt by its CID.
	Get(context.Context, cid.Cid) (ucan.Receipt, error)
	// GetByRan retrieves a receipt by "ran" CID.
	GetByRan(context.Context, cid.Cid) (ucan.Receipt, error)
	// Put adds or replaces a receipt in the store.
	Put(context.Context, ucan.Receipt) error
}

// RanLinkIndex maps "ran" links to receipt root links.
type RanLinkIndex interface {
	Put(ctx context.Context, ran cid.Cid, lnk cid.Cid) error
	Get(ctx context.Context, ran cid.Cid) (cid.Cid, error)
}

// KeyEncoder defines how to encode keys for a specific backend.
type KeyEncoder interface {
	EncodeKey(link cid.Cid) string
}

// Store implements ReceiptStore backed by genericstore.
type Store struct {
	store        *genericstore.Store[ucan.Receipt]
	ranLinkIndex RanLinkIndex
	encoder      KeyEncoder
}

var _ ReceiptStore = (*Store)(nil)

// New creates a ReceiptStore with the given backend,  key encoder, and ran link index.
func New(backend objectstore.ListableStore, encoder KeyEncoder, ranLinkIndex RanLinkIndex) *Store {
	return &Store{
		store:        genericstore.New[ucan.Receipt](backend, Codec{}),
		ranLinkIndex: ranLinkIndex,
		encoder:      encoder,
	}
}

func (s *Store) Get(ctx context.Context, link cid.Cid) (ucan.Receipt, error) {
	rcpt, err := s.store.Get(ctx, s.encoder.EncodeKey(link))
	if err != nil {
		return nil, fmt.Errorf("getting receipt: %w", err)
	}
	return rcpt, nil
}

func (s *Store) GetByRan(ctx context.Context, ran cid.Cid) (ucan.Receipt, error) {
	root, err := s.ranLinkIndex.Get(ctx, ran)
	if err != nil {
		return nil, fmt.Errorf("looking up root by ran: %w", err)
	}
	// Convert cid.Cid to cid.Cid for the key encoder
	rcpt, err := s.store.Get(ctx, s.encoder.EncodeKey(root))
	if err != nil {
		return nil, fmt.Errorf("getting receipt: %w", err)
	}
	return rcpt, nil
}

func (s *Store) Put(ctx context.Context, rcpt ucan.Receipt) error {
	err := s.store.Put(ctx, s.encoder.EncodeKey(rcpt.Link()), rcpt)
	if err != nil {
		return fmt.Errorf("storing receipt: %w", err)
	}
	err = s.ranLinkIndex.Put(ctx, rcpt.Ran(), rcpt.Link())
	if err != nil {
		return fmt.Errorf("indexing receipt by ran: %w", err)
	}
	return nil
}

// Codec implements genericstore.Codec for *receipt.Receipt.
type Codec struct{}

func (Codec) Encode(rcpt ucan.Receipt) ([]byte, error) {
	return rcpt.Bytes(), nil
}

func (Codec) Decode(data []byte) (ucan.Receipt, error) {
	out := new(receipt.Receipt)
	if err := out.UnmarshalCBOR(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("decoding receipt: %w", err)
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

// S3RanLinkIndex implements RanLinkIndex using S3/MinIO storage.
type S3RanLinkIndex struct {
	store  *minio.Store
	prefix string
}

func (idx *S3RanLinkIndex) Put(ctx context.Context, ran cid.Cid, lnk cid.Cid) error {
	key := idx.prefix + ran.String() + ".ref"
	cidStr := lnk.String()
	return idx.store.Put(ctx, key, uint64(len(cidStr)), strings.NewReader(cidStr))
}

func (idx *S3RanLinkIndex) Get(ctx context.Context, ran cid.Cid) (cid.Cid, error) {
	key := idx.prefix + ran.String() + ".ref"
	obj, err := idx.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, objectstore.ErrNotExist) {
			return cid.Undef, store.ErrNotFound
		}
		return cid.Undef, err
	}
	defer obj.Body().Close()
	data, err := io.ReadAll(obj.Body())
	if err != nil {
		return cid.Undef, err
	}
	return cid.Parse(string(data))
}

// DatastoreRanLinkIndex implements RanLinkIndex using datastore.
type DatastoreRanLinkIndex struct {
	ds datastore.Datastore
}

func (idx *DatastoreRanLinkIndex) Put(ctx context.Context, ran cid.Cid, lnk cid.Cid) error {
	return idx.ds.Put(ctx, datastore.NewKey(ran.String()), lnk.Bytes())
}

func (idx *DatastoreRanLinkIndex) Get(ctx context.Context, ran cid.Cid) (cid.Cid, error) {
	data, err := idx.ds.Get(ctx, datastore.NewKey(ran.String()))
	if err != nil {
		return cid.Undef, err
	}
	return cid.Cast(data)
}

// NewS3Store creates a ReceiptStore for S3/MinIO backends.
// Receipts are stored with prefix "receipts/" and ran index with "receipts-ran/".
func NewS3Store(backend *minio.Store) *Store {
	return New(
		backend,
		S3KeyEncoder{},
		&S3RanLinkIndex{store: backend, prefix: "receipts-ran/"},
	)
}

// NewDatastoreStore creates a ReceiptStore for LevelDB/datastore backends.
func NewDatastoreStore(ds datastore.Datastore) *Store {
	receiptsDs := namespace.Wrap(ds, datastore.NewKey("receipts/"))
	ranIndexDs := namespace.Wrap(ds, datastore.NewKey("ranLinkIndex/"))
	return New(
		dsadapter.New(receiptsDs),
		DatastoreKeyEncoder{},
		&DatastoreRanLinkIndex{ds: ranIndexDs},
	)
}
