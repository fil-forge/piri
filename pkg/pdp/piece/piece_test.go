package piece_test

import (
	"bytes"
	"io"
	"math/rand"
	"testing"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/pdp/piece"
)

// fixedCommP builds a deterministic 10KiB commP for use as a stable
// fixture. Returns the data commitment + the unpadded input size.
func fixedCommP(t *testing.T, seed int64, size int) (commD []byte, unpadded uint64) {
	t.Helper()
	r := rand.New(rand.NewSource(seed))
	data := make([]byte, size)
	_, err := io.ReadFull(r, data)
	require.NoError(t, err)

	c := &commp.Calc{}
	_, err = io.Copy(c, bytes.NewReader(data))
	require.NoError(t, err)
	commD, _, err = c.Digest()
	require.NoError(t, err)
	return commD, uint64(size)
}

func TestPieceRoundTrip(t *testing.T) {
	commD, unpadded := fixedCommP(t, 1, 10*1024)

	p, err := piece.FromCommitmentAndSize(commD, unpadded)
	require.NoError(t, err)
	require.Equal(t, commD, p.DataCommitment())

	c := p.CID()
	require.Equal(t, uint64(multicodec.Raw), c.Prefix().Codec)
	require.Equal(t, uint64(multicodec.Fr32Sha256Trunc254Padbintree), c.Prefix().MhType)

	got, err := piece.FromCID(c)
	require.NoError(t, err)
	require.Equal(t, p.Padding(), got.Padding())
	require.Equal(t, p.Height(), got.Height())
	require.Equal(t, p.PaddedSize(), got.PaddedSize())
	require.Equal(t, p.DataCommitment(), got.DataCommitment())
}

// TestPieceWireCompat verifies the piri-internal Piece produces a CID
// byte-identical to the canonical go-fil-commcid implementation. This
// is the wire-compat guarantee: any existing v2 piece CID written by
// libstoracha (or by another commcid-based actor) decodes the same
// way through our type.
func TestPieceWireCompat(t *testing.T) {
	cases := []struct {
		name string
		seed int64
		size int
	}{
		{"10KiB", 1, 10 * 1024},
		{"127B", 2, 127},
		{"1MiB", 3, 1 << 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commD, unpadded := fixedCommP(t, tc.seed, tc.size)

			oracleCID, err := commcid.DataCommitmentToPieceCidv2(commD, unpadded)
			require.NoError(t, err)

			p, err := piece.FromCommitmentAndSize(commD, unpadded)
			require.NoError(t, err)
			require.Equal(t, oracleCID.String(), p.CID().String(), "wire format diverges from go-fil-commcid")
			require.Equal(t, oracleCID.Bytes(), p.CID().Bytes())
			require.Equal(t, []byte(oracleCID.Hash()), []byte(p.Multihash()))
		})
	}
}

func TestFromCIDRejectsWrongCodec(t *testing.T) {
	commD, unpadded := fixedCommP(t, 4, 10*1024)
	p, err := piece.FromCommitmentAndSize(commD, unpadded)
	require.NoError(t, err)

	bad := cid.NewCidV1(uint64(multicodec.DagCbor), p.Multihash())
	_, err = piece.FromCID(bad)
	require.Error(t, err)
}

func TestFromMultihashRejectsWrongCode(t *testing.T) {
	mh, err := multihash.Sum([]byte("not a piece"), multihash.SHA2_256, -1)
	require.NoError(t, err)
	_, err = piece.FromMultihash(mh)
	require.Error(t, err)
}

func TestSizeHelpers(t *testing.T) {
	// Fr32PaddedSizeToV1TreeHeight: sanity boundaries.
	require.Equal(t, uint8(0), piece.Fr32PaddedSizeToV1TreeHeight(32))
	require.Equal(t, uint8(1), piece.Fr32PaddedSizeToV1TreeHeight(64))
	require.Equal(t, uint8(2), piece.Fr32PaddedSizeToV1TreeHeight(128))
	require.Equal(t, uint8(2), piece.Fr32PaddedSizeToV1TreeHeight(65)) // rounds up

	require.Equal(t, uint64(32)<<10, piece.HeightToPaddedSize(10))
	require.Equal(t, uint64(127), piece.MaxDataSize(128))
}
