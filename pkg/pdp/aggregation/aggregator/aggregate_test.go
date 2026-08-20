package aggregator_test

import (
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"testing"
	"time"

	"github.com/filecoin-project/go-data-segment/merkletree"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/stretchr/testify/require"

	libpiece "github.com/fil-forge/libforge/piece"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/aggregator"
)

func TestAggregate(t *testing.T) {
	oddsShrink := 0.8
	oddsReduction := 0.8
	maxSize := 28 // 256 mb
	minSize := 16 // 64 kb
	pieces, err := generatePieces(uint8(maxSize), oddsShrink, oddsReduction, uint8(minSize))
	require.NoError(t, err)
	out, err := json.MarshalIndent(pieces, "", "  ")
	require.NoError(t, err)
	t.Log("piece links\n", string(out))
	agg, err := aggregator.NewAggregate(pieces)
	require.NoError(t, err)

	// Aggregate fields are cid.Cid; convert back to Piece to use
	// DataCommitment() and friends.
	rootPiece, err := libpiece.FromCID(agg.Root)
	require.NoError(t, err)
	rootNode := (*merkletree.Node)(rootPiece.DataCommitment())
	for _, aggPiece := range agg.Pieces {
		piecePL, err := libpiece.FromCID(aggPiece.Link)
		require.NoError(t, err)
		subTree := (*merkletree.Node)(piecePL.DataCommitment())
		require.NoError(t, aggPiece.InclusionProof.ValidateSubtree(subTree, rootNode))
	}
}

// this generates a random series of pieces decaying in size that should add up to a size between 2^(height-1) and 2^(height)
func generatePieces(height uint8, oddsShrink float64, oddsReduction float64, smallestSize uint8) ([]libpiece.Piece, error) {
	size := 0
	targetSize := 1 << (height - 1)
	currentHeight := height
	var pieces []libpiece.Piece
	for size <= targetSize {
		for {
			if currentHeight <= smallestSize {
				break
			}
			shouldShrink := rand.Float64() < oddsShrink
			if !shouldShrink {
				break
			}
			currentHeight--
			oddsShrink = oddsShrink * oddsReduction
		}
		paddedSize := 1 << currentHeight
		blobSize := paddedSize/2 + rand.Intn((paddedSize/2)+1-paddedSize/128)

		randLimited := io.LimitReader(rand.New(rand.NewSource(time.Now().UnixNano())), int64(blobSize))
		cp := &commp.Calc{}
		_, err := io.Copy(cp, randLimited)
		if err != nil {
			return nil, err
		}
		commP, actualSize, err := cp.Digest()
		if err != nil {
			return nil, err
		}
		if actualSize != uint64(paddedSize) {
			return nil, errors.New("calculated wrong")
		}
		p, err := libpiece.FromCommitmentAndSize(commP, uint64(blobSize))
		if err != nil {
			return nil, err
		}
		pieces = append(pieces, p)
		size += paddedSize
	}
	return pieces, nil
}
