package receiptstore

import (
	"testing"

	"github.com/fil-forge/libforge/commands"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan/command"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/receipt"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/store"
)

func testReceiptStore(t *testing.T, newStore func(*testing.T) ReceiptStore) {
	issuer := testutil.RandomIssuer(t)
	task := func(t *testing.T) *invocation.Invocation {
		inv, err := invocation.Invoke(issuer, testutil.RandomDID(t), command.New("/whatever"), &commands.Unit{})
		require.NoError(t, err)
		return inv
	}

	t.Run("roundtrip", func(t *testing.T) {
		s := newStore(t)
		inv := task(t)
		rcpt, err := receipt.IssueOK(issuer, inv.Link(), &commands.Unit{})
		require.NoError(t, err)
		require.NoError(t, s.Put(t.Context(), rcpt))

		got, err := s.Get(t.Context(), rcpt.Link())
		require.NoError(t, err)
		require.Equal(t, rcpt.Link(), got.Link())

		got, err = s.GetByRan(t.Context(), inv.Link())
		require.NoError(t, err)
		require.Equal(t, rcpt.Link(), got.Link())
	})

	t.Run("not found", func(t *testing.T) {
		s := newStore(t)
		_, err := s.Get(t.Context(), testutil.RandomCID(t))
		require.ErrorIs(t, err, store.ErrNotFound)
		_, err = s.GetByRan(t.Context(), testutil.RandomCID(t))
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("a later receipt for the task is found by it", func(t *testing.T) {
		s := newStore(t)
		inv := task(t)
		first, err := receipt.IssueOK(issuer, inv.Link(), &commands.Unit{})
		require.NoError(t, err)
		second, err := receipt.IssueOK(issuer, inv.Link(), &commands.Unit{}, receipt.WithNonce([]byte("second")))
		require.NoError(t, err)
		require.NotEqual(t, first.Link(), second.Link())
		require.NoError(t, s.Put(t.Context(), first))
		require.NoError(t, s.Put(t.Context(), second))

		got, err := s.GetByRan(t.Context(), inv.Link())
		require.NoError(t, err)
		require.Equal(t, second.Link(), got.Link())
	})
}

func TestReceiptStore(t *testing.T) { forEachImpl(t, testReceiptStore) }
