package verifyread

import (
	"bytes"
	"errors"
	"fmt"
	"hash"
	"io"
)

var (
	ErrHashMismatch = errors.New("hash validation failed")
	ErrSizeMismatch = errors.New("size validation failed")
)

type Reader struct {
	src         io.Reader
	h           hash.Hash
	expectedSum []byte

	// expectedSize, when non-zero, is the exact number of bytes the source
	// must yield. Enforced in both directions.
	expectedSize    uint64
	hasExpectedSize bool

	bytesRead uint64
	done      bool  // reached EOF
	finalErr  error // latched terminal error (e.g., mismatch)
}

// Option configures a Reader.
type Option func(*Reader)

// WithExpectedSize makes the Reader enforce an exact byte count.
//
// Over-length is caught as soon as the source yields one byte too many,
// without consuming the rest — the caller does not have to stream an
// unbounded body to discover it is too big.
//
// Under-length is caught at EOF, and is checked before the digest compare so
// a truncated transfer is reported as ErrSizeMismatch rather than
// ErrHashMismatch. The digest of a short read never matches, so without this
// a dropped connection looks like data corruption and sends operators
// hunting in the wrong place.
func WithExpectedSize(n uint64) Option {
	return func(r *Reader) {
		r.expectedSize = n
		r.hasExpectedSize = true
	}
}

func New(src io.Reader, h hash.Hash, expected []byte, opts ...Option) (*Reader, error) {
	if src == nil {
		return nil, fmt.Errorf("source reader cannot be nil")
	}
	if h == nil {
		return nil, fmt.Errorf("hash function cannot be nil")
	}
	if len(expected) == 0 {
		return nil, fmt.Errorf("expected digest cannot be nil")
	}
	r := &Reader{src: src, h: h, expectedSum: expected}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

func (r *Reader) Read(p []byte) (int, error) {
	if r.finalErr != nil {
		return 0, r.finalErr
	}
	if r.done {
		return 0, io.EOF
	}

	n, err := r.src.Read(p)
	if n > 0 {
		// Stop at the first byte past the limit, before hashing the
		// overflow, so an over-long body is cut off rather than drained.
		if r.hasExpectedSize && r.bytesRead+uint64(n) > r.expectedSize {
			r.finalErr = fmt.Errorf("%w: expected %d bytes, source has more",
				ErrSizeMismatch, r.expectedSize)
			return 0, r.finalErr
		}
		_, innErr := r.h.Write(p[:n])
		if innErr != nil {
			return 0, innErr
		}
		r.bytesRead += uint64(n)
	}

	if err == io.EOF {
		r.done = true
		// Size before digest: a short read never matches the digest, and
		// "truncated" is the more actionable diagnosis than "corrupt".
		if r.hasExpectedSize && r.bytesRead != r.expectedSize {
			r.finalErr = fmt.Errorf("%w: expected %d bytes, got %d",
				ErrSizeMismatch, r.expectedSize, r.bytesRead)
			return n, r.finalErr
		}
		sum := r.h.Sum(nil)
		if !bytes.Equal(sum, r.expectedSum) {
			r.finalErr = fmt.Errorf("%w: expected %x, got %x", ErrHashMismatch, r.expectedSum, sum)
			// return n (might be >0) + the error; caller sees last bytes and the failure
			return n, r.finalErr
		}
		return n, io.EOF
	}
	return n, err
}

func (r *Reader) BytesRead() uint64 { return r.bytesRead }

func (r *Reader) Validated() bool {
	return r.done
}
