package store

import "context"

// Transactor runs work that spans several stores as one transaction.
type Transactor interface {
	// Run runs f so that the writes it makes through the stores commit
	// together or not at all, then runs the work f deferred until the
	// commit.
	Run(ctx context.Context, f func(ctx context.Context) error) error
}

// NoTransaction is the Transactor for stores that keep no database: f runs as
// it is, and work deferred until a commit runs as it is deferred.
type NoTransaction struct{}

var _ Transactor = NoTransaction{}

func (NoTransaction) Run(ctx context.Context, f func(ctx context.Context) error) error {
	return f(ctx)
}
