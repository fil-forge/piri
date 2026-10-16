// Package harmonytx runs work that spans several stores in one harmonydb
// transaction. The transaction travels in the context: a store method run
// inside [DB.Run] joins it, and one run outside opens its own. Work
// that must wait for the data to be committed, such as spawning a task that
// reads it or telling another service about it, is registered with
// [AfterCommit].
package harmonytx

import (
	"context"
	"errors"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/fil-forge/piri/pkg/store"
)

type ctxKey struct{}

type state struct {
	tx          *harmonydb.Tx
	afterCommit []func(context.Context) error
}

// Run runs f in the transaction ctx carries, or in a new transaction on db
// when it carries none. A new transaction commits when f returns nil and is
// retried on a serialization failure, so f must be safe to run again. f
// receives the context to pass to anything it calls.
//
// Once a new transaction commits, the functions registered with AfterCommit
// run in order, and Run returns their errors joined: the data is committed
// even when it returns one.
func Run(ctx context.Context, db *harmonydb.DB, f func(ctx context.Context, tx *harmonydb.Tx) error) error {
	if st, ok := ctx.Value(ctxKey{}).(*state); ok {
		return f(ctx, st.tx)
	}
	st, err := run(ctx, db, f)
	if err != nil {
		return err
	}
	var errs []error
	for _, fn := range st.afterCommit {
		errs = append(errs, fn(ctx))
	}
	return errors.Join(errs...)
}

// run runs f in a new transaction and returns the committed transaction's
// state.
func run(ctx context.Context, db *harmonydb.DB, f func(ctx context.Context, tx *harmonydb.Tx) error) (*state, error) {
	var (
		st   *state
		fErr error
	)
	_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		st = &state{tx: tx}
		fErr = f(context.WithValue(ctx, ctxKey{}, st), tx)
		// harmonydb redacts any error whose text mentions a host, port or
		// URL, so f's error goes back to the caller from here. Only a
		// serialization failure goes to harmonydb, which retries on it.
		if harmonydb.IsErrSerialization(fErr) {
			return false, fErr
		}
		return fErr == nil, nil
	}, harmonydb.OptionRetry())
	if fErr != nil {
		return nil, fErr
	}
	if err != nil {
		return nil, err
	}
	return st, nil
}

// AfterCommit registers fn to run once the transaction ctx carries commits.
// Outside a transaction it runs fn at once, since nothing is left to commit.
func AfterCommit(ctx context.Context, fn func(context.Context) error) error {
	if st, ok := ctx.Value(ctxKey{}).(*state); ok {
		st.afterCommit = append(st.afterCommit, fn)
		return nil
	}
	return fn(ctx)
}

// DB is the store.Transactor on a harmonydb database.
type DB struct {
	db *harmonydb.DB
}

var _ store.Transactor = DB{}

// New returns the Transactor on db.
func New(db *harmonydb.DB) DB {
	return DB{db: db}
}

func (d DB) Run(ctx context.Context, f func(ctx context.Context) error) error {
	return Run(ctx, d.db, func(ctx context.Context, _ *harmonydb.Tx) error {
		return f(ctx)
	})
}
