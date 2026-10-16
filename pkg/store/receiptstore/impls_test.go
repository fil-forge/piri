package receiptstore

import (
	"testing"

	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"

	piritestutil "github.com/fil-forge/piri/pkg/internal/testutil"
)

// forEachImpl runs a store test against each implementation. The Postgres
// one shares a database across the test's stores, which keep apart by the
// random keys every test uses.
func forEachImpl(t *testing.T, test func(*testing.T, func(*testing.T) ReceiptStore)) {
	t.Run("datastore", func(t *testing.T) {
		test(t, func(*testing.T) ReceiptStore {
			return NewDatastoreStore(dssync.MutexWrap(datastore.NewMapDatastore()))
		})
	})
	t.Run("postgres", func(t *testing.T) {
		db := piritestutil.NewHarmonyDB(t)
		test(t, func(*testing.T) ReceiptStore {
			return NewPostgresStore(db)
		})
	})
}
