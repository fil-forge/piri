// Package pdpfake provides in-memory fakes of the PDP storage backend for tests.
//
// The fakes satisfy types.PieceAPI and commp.Calculator so that fxtest
// applications can wire the UCAN handlers' PDP code path without pulling in the
// real PDP service (which requires a database, ethereum client, scheduler, and
// signing service).
package pdpfake

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"sync"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"

	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store"
)

// Pieces is an in-memory fake of types.PieceAPI. Bytes are stored in memory
// keyed by their blob multihash. Tests seed bytes with Put and inspect them
// with Bytes / Has.
//
// WritePieceURL and ReadPieceURL return URLs that tests can configure via
// SetWriteURL / SetReadURL — these URLs are echoed into receipts and may be
// fetched by test HTTP servers, so they need to be real HTTP URLs when the
// test does live transfers (e.g. the replicator).
type Pieces struct {
	mu       sync.Mutex
	data     map[string][]byte
	uploads  map[uuid.UUID]multihash.Multihash
	writeURL url.URL
	readURL  url.URL
	removed  []multihash.Multihash
}

// NewPieces returns an empty in-memory Pieces fake. Default URLs use the
// `pdpfake` scheme so a missing SetWriteURL/SetReadURL in a test is obvious.
func NewPieces() *Pieces {
	return &Pieces{
		data:     map[string][]byte{},
		uploads:  map[uuid.UUID]multihash.Multihash{},
		writeURL: url.URL{Scheme: "pdpfake", Host: "write"},
		readURL:  url.URL{Scheme: "pdpfake", Host: "read"},
	}
}

// SetWriteURL sets the URL that WritePieceURL returns. Used by replicator
// tests so the returned URL points at a test HTTP sink.
func (p *Pieces) SetWriteURL(u url.URL) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writeURL = u
}

// SetReadURL sets the URL that ReadPieceURL returns. Used by tests that
// assert location-commitment URLs in receipts.
func (p *Pieces) SetReadURL(u url.URL) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.readURL = u
}

// Put seeds bytes for a digest. Used by tests to simulate completed uploads.
func (p *Pieces) Put(digest multihash.Multihash, data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data[digest.HexString()] = append([]byte(nil), data...)
}

// Bytes returns a copy of the bytes stored for digest, or false if absent.
func (p *Pieces) Bytes(digest multihash.Multihash) ([]byte, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	b, ok := p.data[digest.HexString()]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), b...), true
}

// --- types.PieceReaderAPI ---

func (p *Pieces) Has(_ context.Context, blob multihash.Multihash) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.data[blob.HexString()]
	return ok, nil
}

func (p *Pieces) Read(_ context.Context, digest multihash.Multihash, opts ...types.ReadPieceOption) (*types.PieceReader, error) {
	p.mu.Lock()
	b, ok := p.data[digest.HexString()]
	p.mu.Unlock()
	if !ok {
		return nil, store.ErrNotFound
	}

	cfg := types.ReadPieceConfig{}
	cfg.ProcessOptions(opts)

	start := cfg.ByteRange.Start
	end := uint64(len(b))
	if cfg.ByteRange.End != nil {
		end = *cfg.ByteRange.End + 1 // ReadPieceOption ranges are inclusive
	}
	if start > uint64(len(b)) || end > uint64(len(b)) || start > end {
		return nil, fmt.Errorf("pdpfake: range out of bounds: [%d, %d) of %d", start, end, len(b))
	}

	slice := b[start:end]
	// Size must reflect the total blob size, not the slice length — handlers
	// rely on it to compute Content-Range and decide between 200 / 206.
	return &types.PieceReader{
		Size: int64(len(b)),
		Data: io.NopCloser(bytes.NewReader(slice)),
	}, nil
}

// --- types.PieceWriterAPI ---

func (p *Pieces) AllocatePiece(_ context.Context, alloc types.PieceAllocation) (*types.AllocatedPiece, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, present := p.data[alloc.Piece.Hash.HexString()]; present {
		return &types.AllocatedPiece{
			Allocated: false,
			Piece:     alloc.Piece.Hash,
		}, nil
	}
	id := uuid.New()
	p.uploads[id] = alloc.Piece.Hash
	return &types.AllocatedPiece{
		Allocated: true,
		Piece:     alloc.Piece.Hash,
		UploadID:  id,
	}, nil
}

func (p *Pieces) UploadPiece(_ context.Context, _ types.PieceUpload) error {
	panic("pdpfake: UploadPiece not implemented; tests should seed bytes via Pieces.Put")
}

// --- types.PieceResolverAPI ---

func (p *Pieces) Resolve(_ context.Context, _ multihash.Multihash) (multihash.Multihash, bool, error) {
	panic("pdpfake: Resolve not implemented")
}

// ResolveToPiece reports "not found" for every blob — the fake does not
// compute commps. Tests that exercise pdp.Info must compute their own.
func (p *Pieces) ResolveToPiece(_ context.Context, _ multihash.Multihash) (multihash.Multihash, bool, error) {
	return nil, false, nil
}

func (p *Pieces) ResolveToBlob(_ context.Context, _ multihash.Multihash) (multihash.Multihash, bool, error) {
	panic("pdpfake: ResolveToBlob not implemented")
}

// --- types.PieceCommPAPI ---

func (p *Pieces) CalculateCommP(_ context.Context, _ multihash.Multihash) (types.CalculateCommPResponse, error) {
	panic("pdpfake: CalculateCommP not implemented")
}

// --- types.PieceRemoverAPI ---

// RemovePiece deletes the bytes for blob and records the call so tests can
// assert removal was requested. Idempotent, like the real implementation.
func (p *Pieces) RemovePiece(_ context.Context, blob multihash.Multihash) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.data, blob.HexString())
	p.removed = append(p.removed, blob)
	return nil
}

// ProcessPendingRemovals is a no-op — the fake removes bytes immediately.
func (p *Pieces) ProcessPendingRemovals(_ context.Context) error {
	return nil
}

// Removed returns the digests RemovePiece has been called with.
func (p *Pieces) Removed() []multihash.Multihash {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]multihash.Multihash(nil), p.removed...)
}

// --- extras on types.PieceAPI ---

func (p *Pieces) ParkPiece(_ context.Context, _ types.ParkPieceRequest) error {
	panic("pdpfake: ParkPiece not implemented")
}

func (p *Pieces) WritePieceURL(_ uuid.UUID) (url.URL, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writeURL, nil
}

func (p *Pieces) ReadPieceURL(_ cid.Cid) (url.URL, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.readURL, nil
}

// Compile-time check that Pieces satisfies the surface handlers depend on.
var (
	_ types.PieceAPI        = (*Pieces)(nil)
	_ types.PieceRemoverAPI = (*Pieces)(nil)
)
