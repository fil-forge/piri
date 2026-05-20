package piece

import (
	"fmt"
	"math/bits"
)

// Fr32PaddedSizeToV1TreeHeight returns the binary-tree height for an
// FR32-padded piece of the given size. Sizes that aren't a power of two
// round up under the assumption the tree is padded with zeros.
func Fr32PaddedSizeToV1TreeHeight(size uint64) uint8 {
	if size <= 32 {
		return 0
	}

	b := 63 - bits.LeadingZeros64(size)
	b -= 5 // 2^5 == 32-byte leaves

	if 32<<b < size {
		b++
	}
	return uint8(b)
}

// UnpaddedSizeToV1TreeHeightAndPadding returns the tree height and the
// amount of zero-padding (in bytes of unpadded data) needed to reach that
// height, given an unpadded input size.
func UnpaddedSizeToV1TreeHeightAndPadding(dataSize uint64) (uint8, uint64, error) {
	if dataSize*128 < dataSize {
		return 0, 0, fmt.Errorf("unsupported size: too big")
	}
	if dataSize < 127 {
		return 0, 0, fmt.Errorf("unsupported size: too small")
	}

	fr32DataSize := dataSize * 128 / 127
	if fr32DataSize*127 != dataSize*128 {
		fr32DataSize++
	}

	treeHeight := Fr32PaddedSizeToV1TreeHeight(fr32DataSize)
	paddedFr32DataSize := HeightToPaddedSize(treeHeight)
	paddedDataSize := paddedFr32DataSize / 128 * 127
	padding := paddedDataSize - dataSize

	return treeHeight, padding, nil
}

// HeightToPaddedSize is the FR32-padded tree size for a given height.
func HeightToPaddedSize(height uint8) uint64 {
	return uint64(32) << height
}

// MaxDataSize is the largest unpadded data size that fits in a tree of
// the given padded size.
func MaxDataSize(size uint64) uint64 {
	return size / 128 * 127
}
