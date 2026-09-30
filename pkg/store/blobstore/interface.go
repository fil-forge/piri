package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/multiformats/go-multihash"
)

// ErrDataInconsistent is returned when the data being written does not hash to
// the expected value.
var ErrDataInconsistent = errors.New("data consistency check failed")

// ErrTooLarge is returned when the data being written is larger than expected.
var ErrTooLarge = errors.New("payload too large")

// ErrTooSmall is returned when the data being written is smaller than expected.
var ErrTooSmall = errors.New("payload too small")

// RangeNotSatisfiableError is returned when the byte range option falls outside
// of the total size of the blob.
type RangeNotSatisfiableError struct {
	Range Range
}

func NewRangeNotSatisfiableError(r Range) RangeNotSatisfiableError {
	return RangeNotSatisfiableError{Range: r}
}

func (e RangeNotSatisfiableError) Error() string {
	var rangeStr string
	if e.Range.End != nil {
		rangeStr = fmt.Sprintf("%d-%d", e.Range.Start, *e.Range.End)
	} else {
		rangeStr = fmt.Sprintf("%d-", e.Range.Start)
	}
	return fmt.Sprintf("range not satisfiable: %s", rangeStr)
}

// GetOption is an option configuring byte retrieval from a blobstore.
type GetOption func(cfg *GetOptions) error

type Range struct {
	// Start is the byte to start extracting from (inclusive).
	Start uint64
	// End is the byte to stop extracting at (inclusive).
	End *uint64
}

type GetOptions struct {
	ByteRange Range
}

func (o *GetOptions) ProcessOptions(opts []GetOption) {
	for _, opt := range opts {
		opt(o)
	}
}

func (o *GetOptions) Range() Range {
	return o.ByteRange
}

// WithRange configures a byte range to extract.
func WithRange(start uint64, end *uint64) GetOption {
	return func(opts *GetOptions) error {
		opts.ByteRange = Range{start, end}
		return nil
	}
}

type Object interface {
	// Size returns the total size of the object in bytes.
	Size() int64
	Body() io.ReadCloser
}

type Blobstore interface {
	// Get retrieves the object identified by the passed digest. Returns nil and
	// [ErrNotFound] if the object does not exist.
	//
	// Note: data is not hashed on read.
	Get(ctx context.Context, digest multihash.Multihash, opts ...GetOption) (Object, error)
	// Put stores the bytes to the store and ensures it hashes to the passed
	// digest.
	Put(ctx context.Context, digest multihash.Multihash, size uint64, body io.Reader) error
	// Delete removes the object identified by the passed digest.
	Delete(ctx context.Context, digest multihash.Multihash) error

	// PutUpload stores an upload whose digest is not yet known under a key
	// named by id, the upload's identifier. The bytes stay there once their
	// digest is known: an [UploadIndex] maps the digest to id, and
	// [WithUploads] reads through it, until [Blobstore.Settle] moves them.
	PutUpload(ctx context.Context, id string, size uint64, body io.Reader) error
	// GetUpload retrieves the upload id. Returns [ErrNotFound] if it does not
	// exist.
	GetUpload(ctx context.Context, id string, opts ...GetOption) (Object, error)
	// DeleteUpload removes the upload id. Deleting an upload that does not
	// exist succeeds.
	DeleteUpload(ctx context.Context, id string) error
	// Settle moves the bytes of digest from the upload that holds them to the
	// key of digest, handing them to read on the way so the move costs no
	// read of its own. read gets the blob's bytes and size; the move
	// completes once read returns without error. It reports false, without
	// calling read, when no upload holds the blob, which is always so for a
	// store without an [UploadIndex].
	Settle(ctx context.Context, digest multihash.Multihash, read func(r io.Reader, size int64) error) (bool, error)
}

type GetConfig interface {
	ProcessOptions([]GetOption)
	Range() Range
}

func NewGetConfig() GetConfig {
	return &GetOptions{}
}
