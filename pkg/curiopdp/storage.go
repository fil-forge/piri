// Package curiopdp adapts Piri's storage to the storage interfaces that Curio's
// tasks/pdpv0 pipeline depends on, so Piri can run the shared prove / proving-period
// tasks over its own S3 blobstore.
package curiopdp

import (
	"context"
	"fmt"
	"io"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"

	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/tasks/pdpv0"

	"github.com/fil-forge/piri/pkg/store/blobstore"
)

// Compile-time proof that Piri's adapters satisfy Curio's pdpv0 storage seams.
var (
	_ pdpv0.PieceReader     = (*S3PieceReader)(nil)
	_ pdpv0.ProofCacheStore = NullProofCache{}
)

// blobResolver resolves a piece (commP) multihash to the underlying blob multihash
// under which its bytes are stored. Satisfied by Piri's *piece.StoreResolver.
type blobResolver interface {
	ResolveToBlob(ctx context.Context, piece multihash.Multihash) (multihash.Multihash, bool, error)
}

// S3PieceReader implements pdpv0.PieceReader over Piri's blobstore (S3/MinIO),
// giving the shared prove/save-cache tasks random-access reads of piece bytes.
type S3PieceReader struct {
	store    blobstore.Blobstore
	resolver blobResolver
}

func NewS3PieceReader(store blobstore.Blobstore, resolver blobResolver) *S3PieceReader {
	return &S3PieceReader{store: store, resolver: resolver}
}

func (s *S3PieceReader) GetSharedPieceReader(ctx context.Context, pieceCid cid.Cid, retrieval bool) (storiface.Reader, uint64, error) {
	// NOTE(e2e): pdpv0 may pass a v2 (sized) commP CID; if ResolveToBlob misses,
	// a v2->v1 conversion retry may be needed. Validate against a real dataset.
	blob, ok, err := s.resolver.ResolveToBlob(ctx, pieceCid.Hash())
	if err != nil {
		return nil, 0, fmt.Errorf("resolve piece %s to blob: %w", pieceCid, err)
	}
	if !ok {
		return nil, 0, fmt.Errorf("no blob mapping for piece %s", pieceCid)
	}
	// Get is lazy; Size() triggers a StatObject. We only need the size here.
	obj, err := s.store.Get(ctx, blob)
	if err != nil {
		return nil, 0, fmt.Errorf("stat blob for piece %s: %w", pieceCid, err)
	}
	size := obj.Size()
	_ = obj.Body().Close()
	if size < 0 {
		return nil, 0, fmt.Errorf("negative blob size for piece %s", pieceCid)
	}
	return &s3SectionReader{ctx: ctx, store: s.store, blob: blob, size: size}, uint64(size), nil
}

// s3SectionReader is a storiface.Reader (Read/ReadAt/Seek/Close) backed by ranged
// blobstore GETs. Each Read/ReadAt issues one range request: correct, but
// request-per-call (acceptable — prove reads small sections via the cache path;
// the no-cache fallback reads sequentially in large chunks).
type s3SectionReader struct {
	ctx   context.Context
	store blobstore.Blobstore
	blob  multihash.Multihash
	size  int64
	off   int64
}

func (r *s3SectionReader) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 {
		return 0, fmt.Errorf("s3SectionReader.ReadAt: negative offset")
	}
	if off >= r.size {
		return 0, io.EOF
	}
	end := off + int64(len(p)) - 1 // blobstore ranges are inclusive on both ends
	if end >= r.size {
		end = r.size - 1
	}
	eu := uint64(end)
	obj, err := r.store.Get(r.ctx, r.blob, blobstore.WithRange(uint64(off), &eu))
	if err != nil {
		return 0, err
	}
	body := obj.Body()
	defer body.Close()
	n, err := io.ReadFull(body, p[:end-off+1])
	switch {
	case err == io.ErrUnexpectedEOF:
		err = io.EOF
	case err == nil && n < len(p):
		// caller asked for more than the object holds
		err = io.EOF
	}
	return n, err
}

func (r *s3SectionReader) Read(p []byte) (int, error) {
	n, err := r.ReadAt(p, r.off)
	r.off += int64(n)
	return n, err
}

func (r *s3SectionReader) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.off + offset
	case io.SeekEnd:
		abs = r.size + offset
	default:
		return 0, fmt.Errorf("s3SectionReader.Seek: invalid whence %d", whence)
	}
	if abs < 0 {
		return 0, fmt.Errorf("s3SectionReader.Seek: negative position")
	}
	r.off = abs
	return abs, nil
}

func (r *s3SectionReader) Close() error { return nil }

// NullProofCache implements pdpv0.ProofCacheStore as a no-op: GetPDPLayerIndex
// always reports "not cached", so the prove task takes its full-read memtree
// fallback (genSubPieceMemtree) — i.e. Piri's current proving behavior. The
// save_cache task should simply not be registered. Back this with a real store
// (e.g. Postgres) later to pick up the proving optimization.
type NullProofCache struct{}

func (NullProofCache) GetPDPLayerIndex(ctx context.Context, pieceCidV2 cid.Cid) (bool, int, error) {
	return false, 0, nil
}

func (NullProofCache) GetPDPLayer(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int) ([]indexstore.NodeDigest, error) {
	return nil, nil
}

func (NullProofCache) GetPDPNode(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int, index int64) (bool, *indexstore.NodeDigest, error) {
	return false, nil, nil
}

func (NullProofCache) AddPDPLayer(ctx context.Context, pieceCidV2 cid.Cid, layer []indexstore.NodeDigest) error {
	return nil
}
