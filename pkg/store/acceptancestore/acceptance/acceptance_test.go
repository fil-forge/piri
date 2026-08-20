package acceptance_test

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/fil-forge/ucantone/testutil"
	"github.com/fil-forge/ucantone/ucan/promise"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
)

func TestRoundtrip(t *testing.T) {
	a := acceptance.Acceptance{
		Space: testutil.RandomDID(t),
		Blob: acceptance.Blob{
			Digest: testutil.RandomDigest(t),
			Size:   rand.Uint64N(1000000),
		},
		PDPAccept:  promise.AwaitOK{Task: testutil.RandomCID(t)},
		ExecutedAt: uint64(time.Now().Unix()),
		Cause:      testutil.RandomCID(t),
		Site:       testutil.RandomCID(t),
	}

	buf, err := acceptance.Encode(a)
	require.NoError(t, err)

	a2, err := acceptance.Decode(buf)
	require.NoError(t, err)
	require.Equal(t, a, a2)
}
