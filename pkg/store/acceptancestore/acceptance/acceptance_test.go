package acceptance_test

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/fil-forge/libforge/testutil"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store/acceptancestore/acceptance"
)

func TestRoundtrip(t *testing.T) {
	codec := acceptance.Codec{}

	t.Run("without PDP", func(t *testing.T) {
		a := acceptance.Acceptance{
			Space: testutil.RandomDID(t),
			Blob: acceptance.Blob{
				Digest: testutil.RandomMultihash(t),
				Size:   rand.Uint64N(1000000),
			},
			ExecutedAt: uint64(time.Now().Unix()),
			Cause:      testutil.RandomCID(t),
		}

		buf, err := codec.Encode(a)
		require.NoError(t, err)

		a2, err := codec.Decode(buf)
		require.NoError(t, err)
		require.Equal(t, a, a2)
	})

	t.Run("with PDP", func(t *testing.T) {
		a := acceptance.Acceptance{
			Space: testutil.RandomDID(t),
			Blob: acceptance.Blob{
				Digest: testutil.RandomMultihash(t),
				Size:   rand.Uint64N(1000000),
			},
			PDPAccept: &acceptance.Promise{
				UcanAwait: acceptance.Await{
					Selector: ".out.ok",
					Link:     testutil.RandomCID(t),
				},
			},
			ExecutedAt: uint64(time.Now().Unix()),
			Cause:      testutil.RandomCID(t),
		}

		buf, err := codec.Encode(a)
		require.NoError(t, err)

		a2, err := codec.Decode(buf)
		require.NoError(t, err)
		require.Equal(t, a, a2)
	})
}
