package allocationstore

import (
	"context"
	"testing"
	"time"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/testutil"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

// ReleasePending takes the allocate task link's lock itself, so a caller that
// does not, such as the release of a discarded upload, still waits for an
// accept or a reject that holds it.
func TestReleasePending_TakesTheLinkLock(t *testing.T) {
	db := piritestutil.NewHarmonyDB(t)
	ctx := t.Context()
	s := NewPostgresStore(db)
	p := allocation.Pending{
		Allocation: testutil.RandomCID(t),
		Space:      testutil.RandomDID(t),
		Size:       4,
		DigestCode: multihash.SHA2_256,
		Cause:      testutil.RandomCID(t),
		Expires:    1,
		UploadID:   "upload",
		Digest:     testutil.RandomMultihash(t),
	}
	require.NoError(t, s.PutPending(ctx, p))
	_, err := s.Claim(ctx, allocation.Allocation{
		Space: p.Space, Blob: blob.Blob{Digest: p.Digest, Size: p.Size}, Cause: p.Cause, Allocation: p.Allocation,
	})
	require.NoError(t, err)

	locked, finish := make(chan struct{}), make(chan struct{})
	held := make(chan error, 1)
	go func() {
		held <- harmonytx.Run(ctx, db, func(ctx context.Context, _ *harmonydb.Tx) error {
			if err := s.LockPending(ctx, p.Allocation); err != nil {
				return err
			}
			close(locked)
			<-finish
			return nil
		})
	}()
	<-locked

	released := make(chan error, 1)
	go func() {
		_, err := s.ReleasePending(ctx, p)
		released <- err
	}()
	select {
	case err := <-released:
		t.Fatalf("the release finished while the link was locked: %v", err)
	case <-time.After(time.Second):
	}

	close(finish)
	require.NoError(t, <-held)
	require.NoError(t, <-released)
	_, err = s.GetPending(ctx, p.Allocation)
	require.ErrorIs(t, err, store.ErrNotFound)
}
