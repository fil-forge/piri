package blob

import (
	"context"
	"errors"
	"testing"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/libforge/identity"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/internal/testutil/pdpfake"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
	"github.com/fil-forge/piri/pkg/store/invocationstore"
)

// publishFunc is a publisher.Publisher made of a function.
type publishFunc func(context.Context, ucan.Invocation) error

func (f publishFunc) Publish(ctx context.Context, inv ucan.Invocation) error { return f(ctx, inv) }

type postgresAccept struct {
	deps    AcceptDeps
	pieces  *pdpfake.Pieces
	accepts *acceptancestore.PostgresStore
	allocs  *allocationstore.PostgresStore
	claims  *invocationstore.PostgresStore
}

func newPostgresAccept(t *testing.T, pub publishFunc) postgresAccept {
	t.Helper()
	db := piritestutil.NewHarmonyDB(t)
	p := postgresAccept{
		pieces:  pdpfake.NewPieces(),
		accepts: acceptancestore.NewPostgresStore(db),
		allocs:  allocationstore.NewPostgresStore(db),
		claims:  invocationstore.NewPostgresStore(db),
	}
	p.deps = AcceptDeps{
		ID:          identity.Identity{Issuer: testutil.WebService},
		Acceptances: p.accepts,
		Pieces:      p.pieces,
		Commp:       pdpfake.NewCommp(),
		ClaimStore:  p.claims,
		Publisher:   pub,
		Pending:     p.allocs,
		Tx:          harmonytx.New(db),
	}
	return p
}

// accept accepts a blob whose space holds a later allocation than the one
// accepted, so the accept makes the accepted allocation current again.
func (p postgresAccept) accept(t *testing.T) (req *AcceptRequest, later cid.Cid, resp *AcceptResponse, err error) {
	t.Helper()
	ctx := t.Context()
	accepted := testutil.RandomCID(t)
	later = testutil.RandomCID(t)
	req = &AcceptRequest{
		Space:      testutil.RandomDID(t),
		Blob:       blob.Blob{Digest: testutil.RandomMultihash(t), Size: 4},
		Cause:      testutil.RandomCID(t),
		Allocation: &accepted,
	}
	require.NoError(t, p.allocs.Put(ctx, allocation.Allocation{
		Allocation: later,
		Space:      req.Space,
		Blob:       req.Blob,
		Cause:      testutil.RandomCID(t),
	}))
	p.pieces.Put(req.Blob.Digest, []byte("data"))
	resp, err = Accept(ctx, p.deps, req)
	return req, later, resp, err
}

// A step that fails inside the accept's transaction leaves none of its
// records: no acceptance, no claim, and the space's allocation as it was.
func TestAccept_FailureLeavesNoRecords(t *testing.T) {
	var claim ucan.Invocation
	p := newPostgresAccept(t, func(_ context.Context, inv ucan.Invocation) error {
		claim = inv
		return errors.New("queue unavailable")
	})
	ctx := t.Context()

	req, later, _, err := p.accept(t)
	require.ErrorContains(t, err, "queue unavailable")

	_, err = p.accepts.Get(ctx, req.Blob.Digest, req.Space)
	require.ErrorIs(t, err, store.ErrNotFound, "the acceptance is rolled back")
	_, err = p.claims.Get(ctx, claim.Link())
	require.ErrorIs(t, err, store.ErrNotFound, "the claim is rolled back")
	alloc, err := p.allocs.Get(ctx, req.Blob.Digest, req.Space)
	require.NoError(t, err)
	require.Equal(t, later, alloc.Allocation, "the later allocation stays current")
}

// Work registered to run after the commit, such as caching the claim with
// the indexing service, sees the accept's records committed, and its failure
// fails the accept without undoing them.
func TestAccept_AfterCommitSeesRecords(t *testing.T) {
	var p postgresAccept
	p = newPostgresAccept(t, func(ctx context.Context, inv ucan.Invocation) error {
		return harmonytx.AfterCommit(ctx, func(ctx context.Context) error {
			if _, err := p.claims.Get(ctx, inv.Link()); err != nil {
				return err
			}
			return errors.New("indexer unavailable")
		})
	})
	ctx := t.Context()

	req, _, _, err := p.accept(t)
	require.ErrorContains(t, err, "indexer unavailable", "the claim was committed before the cache ran")

	_, err = p.accepts.Get(ctx, req.Blob.Digest, req.Space)
	require.NoError(t, err, "the acceptance stands")
	alloc, err := p.allocs.Get(ctx, req.Blob.Digest, req.Space)
	require.NoError(t, err)
	require.Equal(t, *req.Allocation, alloc.Allocation, "the accepted allocation is current")
}

func TestAccept_CommitsRecords(t *testing.T) {
	p := newPostgresAccept(t, func(context.Context, ucan.Invocation) error { return nil })
	ctx := t.Context()

	req, _, resp, err := p.accept(t)
	require.NoError(t, err)

	acc, err := p.accepts.Get(ctx, req.Blob.Digest, req.Space)
	require.NoError(t, err)
	require.Equal(t, resp.Claim.Link(), acc.Site)
	_, err = p.claims.Get(ctx, resp.Claim.Link())
	require.NoError(t, err)
	alloc, err := p.allocs.Get(ctx, req.Blob.Digest, req.Space)
	require.NoError(t, err)
	require.Equal(t, *req.Allocation, alloc.Allocation)
}
