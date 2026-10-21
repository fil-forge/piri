package harmonytx_test

import (
	"context"
	"errors"
	"testing"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	"github.com/fil-forge/piri/pkg/internal/testutil"
)

func insert(ctx context.Context, db *harmonydb.DB, id string) error {
	return harmonytx.Run(ctx, db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`INSERT INTO ucan_invocations (cid, bytes) VALUES ($1, '\x00')`, id)
		return err
	})
}

func exists(t *testing.T, db *harmonydb.DB, id string) bool {
	t.Helper()
	var ok bool
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT EXISTS (SELECT 1 FROM ucan_invocations WHERE cid = $1)`, id).Scan(&ok))
	return ok
}

func TestRun(t *testing.T) {
	db := testutil.NewHarmonyDB(t)
	tr := harmonytx.New(db)

	t.Run("work joined to a transaction commits with it", func(t *testing.T) {
		var ran []string
		err := tr.Run(t.Context(), func(ctx context.Context) error {
			if err := insert(ctx, db, "joined-a"); err != nil {
				return err
			}
			if err := harmonytx.AfterCommit(ctx, func(context.Context) error {
				ran = append(ran, "first")
				return nil
			}); err != nil {
				return err
			}
			require.Empty(t, ran, "after-commit work waits for the commit")
			if err := harmonytx.AfterCommit(ctx, func(context.Context) error {
				ran = append(ran, "second")
				return nil
			}); err != nil {
				return err
			}
			return insert(ctx, db, "joined-b")
		})
		require.NoError(t, err)
		require.True(t, exists(t, db, "joined-a"))
		require.True(t, exists(t, db, "joined-b"))
		require.Equal(t, []string{"first", "second"}, ran)
	})

	t.Run("an error rolls back every write and skips after-commit work", func(t *testing.T) {
		// harmonydb redacts errors that mention a port; the caller sees its own.
		boom := errors.New("unsupported transport")
		ran := false
		err := tr.Run(t.Context(), func(ctx context.Context) error {
			if err := insert(ctx, db, "rolled-back"); err != nil {
				return err
			}
			if err := harmonytx.AfterCommit(ctx, func(context.Context) error {
				ran = true
				return nil
			}); err != nil {
				return err
			}
			return boom
		})
		require.ErrorIs(t, err, boom)
		require.False(t, exists(t, db, "rolled-back"))
		require.False(t, ran)
	})

	t.Run("an after-commit error is returned with the data committed", func(t *testing.T) {
		boom := errors.New("indexer unavailable")
		err := tr.Run(t.Context(), func(ctx context.Context) error {
			if err := insert(ctx, db, "committed"); err != nil {
				return err
			}
			return harmonytx.AfterCommit(ctx, func(context.Context) error { return boom })
		})
		require.ErrorIs(t, err, boom)
		require.True(t, exists(t, db, "committed"))
	})

	t.Run("outside a transaction after-commit work runs at once", func(t *testing.T) {
		ran := false
		require.NoError(t, harmonytx.AfterCommit(t.Context(), func(context.Context) error {
			ran = true
			return nil
		}))
		require.True(t, ran)
	})
}
