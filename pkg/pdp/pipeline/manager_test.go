package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
)

// staticConfig keeps BatchSize above anything Submit buffers so flush never
// reaches the AddRoots claim (which needs a running task engine).
type staticConfig struct{ batch uint }

func (c staticConfig) PollInterval() time.Duration { return time.Minute }
func (c staticConfig) BatchSize() uint             { return c.batch }
func (c staticConfig) Subscribe(config.Key, func(old, new any)) (func(), error) {
	return func() {}, nil
}

func testRoot(t *testing.T, seed string) cid.Cid {
	t.Helper()
	h, err := multihash.Sum([]byte(seed), multihash.SHA2_256, -1)
	require.NoError(t, err)
	return cid.NewCidV1(cid.Raw, h)
}

func TestSubmit_BatchInsertIdempotent(t *testing.T) {
	db := piritestutil.NewHarmonyDB(t)
	m := NewSubmissionManager(db, staticConfig{batch: 100}, &AddRootsTask{})
	ctx := context.Background()

	a, b, c := testRoot(t, "a"), testRoot(t, "b"), testRoot(t, "c")

	require.NoError(t, m.Submit(ctx, a, b))
	require.NoError(t, m.Submit(ctx, b, c)) // overlap: ON CONFLICT DO NOTHING
	require.NoError(t, m.Submit(ctx))       // empty submit is a flush-only noop

	var rows []struct {
		Root string `db:"root"`
	}
	require.NoError(t, db.Select(ctx, &rows, `
		SELECT root FROM pdp_root_submissions ORDER BY root
	`))
	roots := make([]string, len(rows))
	for i, r := range rows {
		roots[i] = r.Root
	}
	want := []string{a.String(), b.String(), c.String()}
	require.ElementsMatch(t, want, roots)
}
