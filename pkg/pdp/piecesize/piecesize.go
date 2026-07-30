// Package piecesize is the single source of truth for how large a piece Piri
// will accept, and for converting between the two units the question is asked
// in.
//
// # Units
//
// A blob arrives as some number of raw bytes. Before it can be proven it is
// FR32-padded and laid out as a binary merkle tree whose size is always a
// power of two:
//
//	raw N -> UnpaddedSizeToV1TreeHeightAndPadding(N) -> 32<<height (padded)
//
// Because the padded size rounds up to a power of two, raw caps that sit just
// above 2^k*127/128 are pathological: a raw cap of 268435456 (2^28) pads to
// 2^29, i.e. it costs twice the memory of a cap two MiB lower, which pads to
// exactly 2^28. Limits are therefore configured as a padded power of two and
// the raw cap is derived, never the reverse.
//
// # Where the ceiling comes from
//
// Proving is done by Curio's vendored pdpv0 tasks. For each challenge the
// prove task builds a full in-memory merkle tree over the challenged
// *sub-piece* (tasks/pdpv0/task_prove.go genSubPieceMemtree ->
// lib/proof.BuildSha254Memtree), which hard-errors above
// lib/proof.MaxMemtreeSize. Peak RSS is roughly 3x the padded size (the
// unpadded read buffer plus the ~2x tree buffer during fr32.Pad); Curio
// declares Ram: 3<<30 for that task on exactly this basis.
//
// A proof cache does not lift this. GenerateCachedProof reads only a small
// section, so a cached piece above the ceiling would prove — but any cache
// miss falls back to the full memtree, which fails, and after MaxFailures the
// challenge window is missed and a fault is registered. The ceiling is
// therefore hard regardless of caching.
//
// CurioMaxPaddedSize is derived from Curio's own symbol so that bumping the
// Curio dependency moves this limit here and nowhere else. Note that the
// ceiling constrains sub-pieces only: aggregates are assembled from sub-piece
// commitments without touching bulk data, and are bounded instead by the
// on-chain MAX_PIECE_SIZE_LOG2 (see pkg/pdp/service/roots_add.go).
package piecesize

import (
	"fmt"
	"math/bits"

	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/go-state-types/abi"

	libpiece "github.com/fil-forge/libforge/piece"
)

const (
	// CurioMaxPaddedSize is the hard ceiling on the padded size of a single
	// sub-piece, above which Curio's prove task cannot build a memtree. No
	// configured limit may exceed it.
	CurioMaxPaddedSize uint64 = proof.MaxMemtreeSize

	// MinPaddedSize is the smallest configurable maximum. Below this a node
	// could not store a piece worth aggregating.
	MinPaddedSize uint64 = 1 << 20

	// DefaultMaxPaddedSize is the shipped default, 256 MiB, which yields a
	// raw cap of 266338304 and a memtree peak around 768 MiB. It is
	// deliberately well under CurioMaxPaddedSize: with no proof cache every
	// proof builds a full memtree, so a 1 GiB piece would cost ~3 GiB of RSS
	// per concurrent prove. Raising the default is gated on a real proof
	// cache; operators who have the memory can raise the limit by config.
	DefaultMaxPaddedSize uint64 = 1 << 28
)

// Compile-time assertion that the default fits under Curio's ceiling. If
// DefaultMaxPaddedSize ever exceeds CurioMaxPaddedSize this underflows and
// fails to compile.
const _ = CurioMaxPaddedSize - DefaultMaxPaddedSize

// minPaddedPiece is the smallest padded piece the merkle layout admits; it is
// also the lower bound enforced by the aggregator.
const minPaddedPiece uint64 = 128

// MaxRawForPadded is the largest raw byte count whose merkle tree fits in a
// padded tree of exactly padded bytes. padded is expected to be a power of
// two; callers get it from a validated Limits.
func MaxRawForPadded(padded uint64) uint64 {
	return libpiece.MaxDataSize(padded)
}

// PaddedForRaw is the padded tree size a raw byte count occupies, i.e. the
// value that lands in pdp_data_set_pieces.sub_piece_size and that Curio's
// memtree ceiling is compared against.
func PaddedForRaw(raw uint64) (uint64, error) {
	// The tree layout has no representation below one full leaf-pair; the
	// smallest piece is padded to minPaddedPiece.
	if raw < 127 {
		return minPaddedPiece, nil
	}
	height, _, err := libpiece.UnpaddedSizeToV1TreeHeightAndPadding(raw)
	if err != nil {
		return 0, fmt.Errorf("computing tree height for raw size %d: %w", raw, err)
	}
	return libpiece.HeightToPaddedSize(height), nil
}

// ValidatePaddedSize reports whether padded is usable as a configured
// maximum: a power of two within [MinPaddedSize, CurioMaxPaddedSize].
//
// The power-of-two requirement is not pedantry. A padded tree size is always
// a power of two, so a non-power-of-two limit could never be attained exactly
// — it would silently behave as the next power of two down, and an operator
// setting 384 MiB would get 256 MiB.
func ValidatePaddedSize(padded uint64) error {
	if bits.OnesCount64(padded) != 1 {
		return fmt.Errorf(
			"max padded piece size must be a power of two, got %d (padded tree sizes are always powers of two)",
			padded,
		)
	}
	if padded < MinPaddedSize {
		return fmt.Errorf("max padded piece size %d is below the minimum %d", padded, MinPaddedSize)
	}
	if padded > CurioMaxPaddedSize {
		return fmt.Errorf(
			"max padded piece size %d exceeds the proving limit %d: Curio's prove task builds a full memtree per challenged sub-piece and cannot exceed it",
			padded, CurioMaxPaddedSize,
		)
	}
	return nil
}

// Limits is a resolved piece-size limit. The zero Limits reports the
// defaults, so it is safe to use unset.
type Limits struct {
	// Padded is the maximum padded tree size, a power of two. Zero means
	// DefaultMaxPaddedSize.
	Padded uint64
}

// DefaultLimits is the shipped limit set.
var DefaultLimits = Limits{Padded: DefaultMaxPaddedSize}

// MaxPadded is the maximum padded tree size.
func (l Limits) MaxPadded() uint64 {
	if l.Padded == 0 {
		return DefaultMaxPaddedSize
	}
	return l.Padded
}

// MaxRaw is the maximum raw (pre-padding) byte count a piece may have. This
// is the number to compare an incoming blob size against.
func (l Limits) MaxRaw() uint64 {
	return MaxRawForPadded(l.MaxPadded())
}

// MaxUnpadded is MaxRaw in abi terms, for comparison against abi-typed sizes.
func (l Limits) MaxUnpadded() abi.UnpaddedPieceSize {
	return abi.PaddedPieceSize(l.MaxPadded()).Unpadded()
}

// CheckRaw reports whether a raw byte count is within the limit, returning an
// *ExceededError if not.
func (l Limits) CheckRaw(n uint64) error {
	if max := l.MaxRaw(); n > max {
		return &ExceededError{Size: n, MaxRaw: max, MaxPadded: l.MaxPadded()}
	}
	return nil
}

// ExceededError reports a piece larger than the configured limit.
type ExceededError struct {
	Size      uint64
	MaxRaw    uint64
	MaxPadded uint64
}

func (e *ExceededError) Error() string {
	return fmt.Sprintf("piece size %d exceeds maximum %d (padded limit %d)", e.Size, e.MaxRaw, e.MaxPadded)
}

// Policy is a read-through handle on the currently configured limits, which
// an operator may change at runtime. Consumers hold a Policy rather than a
// Limits so that a config change takes effect without a restart.
//
// The zero Policy reports DefaultLimits, so a struct embedding one is usable
// in a test with no wiring.
type Policy struct {
	read func() Limits
}

// NewPolicy builds a Policy from a function returning the current limits. The
// function is called on every check, not snapshotted.
func NewPolicy(read func() Limits) Policy {
	return Policy{read: read}
}

// Limits returns the currently configured limits.
func (p Policy) Limits() Limits {
	if p.read == nil {
		return DefaultLimits
	}
	return p.read()
}

// MaxRaw is the current maximum raw byte count for a piece.
func (p Policy) MaxRaw() uint64 { return p.Limits().MaxRaw() }

// CheckRaw reports whether a raw byte count is within the current limit.
func (p Policy) CheckRaw(n uint64) error { return p.Limits().CheckRaw(n) }
