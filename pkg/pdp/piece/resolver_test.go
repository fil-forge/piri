package piece_test

import (
	"bytes"
	"io"
	"testing"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/libforge/testutil"

	libpiece "github.com/fil-forge/libforge/piece"
)

// NOTE: the StoreResolver's database path (commP<->blob mapping over
// pdp_piece_mh_to_commp) was migrated from in-memory gorm/sqlite to Curio's
// harmonydb, which is Postgres-only. Its DB-backed tests (and the CalculateCommP
// singleflight tests) were removed with the gorm stack; they need re-homing onto
// a Postgres-backed integration harness (follow-on). This pure test has no DB
// dependency and is retained.
func TestMultihashToCommpV2CID(t *testing.T) {
	var pieceCID, commpCID cid.Cid
	var pieceMH multihash.Multihash
	{
		size := 10 * 1024
		c := &commp.Calc{}
		data := testutil.RandomBytes(t, size)

		n, err := io.Copy(c, bytes.NewReader(data))
		require.NoError(t, err)
		require.EqualValues(t, size, n)

		digest, _, err := c.Digest()
		require.NoError(t, err)

		pieceCID, err = commcid.DataCommitmentToPieceCidv2(digest, uint64(size))
		require.NoError(t, err)
		pieceMH = pieceCID.Hash()
	}

	{
		commpCID = libpiece.MultihashToCommpCID(pieceMH)
	}

	require.Equal(t, pieceCID.String(), commpCID.String())
}
