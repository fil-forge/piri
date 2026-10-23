package ipnistore

import (
	"context"
	"crypto/rand"
	"fmt"
	"slices"
	"testing"

	"github.com/fil-forge/go-ipni-tools/pkg/metadata"
	ipnipub "github.com/fil-forge/go-ipni-tools/pkg/publisher"
	"github.com/fil-forge/libforge/testutil"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/lib"
	"github.com/fil-forge/piri/pkg/database/harmonytx"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/store/objectstore/memory"
)

// BenchmarkPublishBatch publishes batches of one-multihash specs, the shape
// one accepted blob produces, inside one Postgres transaction as
// PublishClaimed does: through the per-item store calls, and through the
// store's batch calls.
func BenchmarkPublishBatch(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("adverts=%d/per-item", n), func(b *testing.B) { benchPublishBatch(b, n, false) })
		b.Run(fmt.Sprintf("adverts=%d/batched", n), func(b *testing.B) { benchPublishBatch(b, n, true) })
	}
}

func benchPublishBatch(b *testing.B, n int, batched bool) {
	db := piritestutil.NewHarmonyDB(b)
	key, err := lib.Libp2pKey(testutil.Alice)
	require.NoError(b, err)
	provider, err := peer.IDFromPrivateKey(key)
	require.NoError(b, err)
	st := New(db, memory.NewStore(), provider)
	var opts []ipnipub.Option
	if batched {
		opts = append(opts, ipnipub.WithBatchStore(st))
	}
	pub, err := ipnipub.New(key, st, opts...)
	require.NoError(b, err)
	tx := harmonytx.New(db)
	ctx := context.Background()

	for b.Loop() {
		b.StopTimer()
		specs := make([]ipnipub.AdvertSpec, n)
		for i := range specs {
			digest := randomMultihash(b)
			specs[i] = ipnipub.AdvertSpec{
				ContextID: cid.NewCidV1(cid.Raw, randomMultihash(b)).String(),
				Digests:   slices.Values([]multihash.Multihash{digest}),
				Metadata:  metadata.MetadataContext.New(&metadata.LocationCommitmentMetadata{Claim: cid.NewCidV1(cid.Raw, digest)}),
			}
		}
		b.StartTimer()
		err := tx.Run(ctx, func(ctx context.Context) error {
			_, err := pub.PublishBatch(ctx, peer.AddrInfo{ID: provider}, specs)
			return err
		})
		require.NoError(b, err)
	}
}

func randomMultihash(b *testing.B) multihash.Multihash {
	buf := make([]byte, 32)
	_, err := rand.Read(buf)
	require.NoError(b, err)
	digest, err := multihash.Sum(buf, multihash.SHA2_256, -1)
	require.NoError(b, err)
	return digest
}
