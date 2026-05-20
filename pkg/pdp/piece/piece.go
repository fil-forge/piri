package piece

import (
	"fmt"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/multiformats/go-varint"
)

// fr32Code is the multihash code for FR32_SHA256_TRUNC254_PADDED_BINARY_TREE
// (FRC-0069). Aliased for use in this package.
const fr32Code = uint64(multicodec.Fr32Sha256Trunc254Padbintree)

// Piece is a Filecoin commP piece reference. Its on-wire form is a
// CIDv1 (Raw codec) wrapping an FR32 multihash whose payload is:
//
//	varint(padding) || height (1 byte) || commitment (32 bytes)
type Piece struct {
	padding    uint64
	height     uint8
	commitment [32]byte
}

func (p Piece) Padding() uint64 { return p.padding }
func (p Piece) Height() uint8   { return p.height }

func (p Piece) DataCommitment() []byte {
	out := make([]byte, 32)
	copy(out, p.commitment[:])
	return out
}

// PaddedSize is the FR32-padded tree size in bytes (32 << height).
func (p Piece) PaddedSize() uint64 { return HeightToPaddedSize(p.height) }

// Multihash builds the FR32 multihash bytes for this piece.
func (p Piece) Multihash() multihash.Multihash {
	paddingSize := varint.UvarintSize(p.padding)
	digestSize := 32 + 1 + paddingSize

	buf := make(
		[]byte,
		varint.UvarintSize(fr32Code)+varint.UvarintSize(uint64(digestSize))+digestSize,
	)
	pos := varint.PutUvarint(buf, fr32Code)
	pos += varint.PutUvarint(buf[pos:], uint64(digestSize))
	pos += varint.PutUvarint(buf[pos:], p.padding)
	buf[pos] = p.height
	pos++
	copy(buf[pos:], p.commitment[:])
	return buf
}

// CID returns the v2 piece CID: CIDv1 with Raw codec wrapping the FR32
// multihash. Bit-identical to libstoracha's PieceLink.Link().
func (p Piece) CID() cid.Cid { return MultihashToCommpCID(p.Multihash()) }

// FromCID parses a v2 piece CID into a Piece. The CID must use the Raw
// codec and an FR32 multihash.
func FromCID(c cid.Cid) (Piece, error) {
	if c.Prefix().Codec != uint64(multicodec.Raw) {
		return Piece{}, fmt.Errorf("piece CID must use raw codec, got 0x%x", c.Prefix().Codec)
	}
	return FromMultihash(c.Hash())
}

// FromMultihash parses an FR32 multihash into a Piece.
func FromMultihash(mh multihash.Multihash) (Piece, error) {
	decoded, err := multihash.Decode(mh)
	if err != nil {
		return Piece{}, fmt.Errorf("decoding multihash: %w", err)
	}
	if decoded.Code != fr32Code {
		return Piece{}, fmt.Errorf("multihash code must be 0x%x, got 0x%x", fr32Code, decoded.Code)
	}

	padding, n, err := varint.FromUvarint(decoded.Digest)
	if err != nil {
		return Piece{}, fmt.Errorf("reading padding varint: %w", err)
	}
	if len(decoded.Digest) < n+1+32 {
		return Piece{}, fmt.Errorf("digest too short: %d bytes", len(decoded.Digest))
	}
	height := decoded.Digest[n]
	var commitment [32]byte
	copy(commitment[:], decoded.Digest[n+1:n+1+32])

	return Piece{padding: padding, height: height, commitment: commitment}, nil
}

// FromCommitmentAndSize builds a Piece from a raw 32-byte data
// commitment and an unpadded input size in bytes.
func FromCommitmentAndSize(commD []byte, unpaddedDataSize uint64) (Piece, error) {
	if len(commD) != 32 {
		return Piece{}, fmt.Errorf("commitments must be 32 bytes long, got %d", len(commD))
	}
	if unpaddedDataSize < 127 {
		return Piece{}, fmt.Errorf("unpadded data size must be at least 127, got %d", unpaddedDataSize)
	}

	height, padding, err := UnpaddedSizeToV1TreeHeightAndPadding(unpaddedDataSize)
	if err != nil {
		return Piece{}, err
	}
	if padding > varint.MaxValueUvarint63 {
		return Piece{}, fmt.Errorf("padding must be less than 2^63-1, got %d", padding)
	}

	var commitment [32]byte
	copy(commitment[:], commD)
	return Piece{padding: padding, height: height, commitment: commitment}, nil
}
