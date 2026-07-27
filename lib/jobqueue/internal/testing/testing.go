// Copyright (c) https://github.com/maragudk/goqite
// https://github.com/maragudk/goqite/blob/6d1bf3c0bcab5a683e0bc7a82a4c76ceac1bbe3f/LICENSE
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree, or at:
// https://opensource.org/licenses/MIT

package testing

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/lib/jobqueue/dedup"
	"github.com/fil-forge/piri/lib/jobqueue/queue"
)

// NewDB creates a new PostgreSQL test-container database connection with both
// the classic queue and dedup queue schemas applied, and all tables truncated
// so each test starts from a clean state. The test is skipped if the
// PostgreSQL container is not available.
func NewDB(t testing.TB) *sql.DB {
	t.Helper()
	db := NewPostgresDB(t)

	// Execute classic queue schema
	_, err := db.Exec(queue.Schema)
	if err != nil {
		t.Fatalf("setup postgres queue schema: %v", err)
	}

	// Execute dedup queue schema
	_, err = db.Exec(dedup.Schema)
	if err != nil {
		t.Fatalf("setup postgres dedup schema: %v", err)
	}

	// Truncate tables to ensure clean state for each test
	_, err = db.Exec(`TRUNCATE TABLE job_dead, job_done, jobs, job_ns, queues, jobqueue_dead, jobqueue CASCADE`)
	if err != nil {
		t.Fatalf("truncate postgres tables: %v", err)
	}
	return db
}

// NewQ creates a new queue backed by the PostgreSQL test container.
func NewQ(t testing.TB, opts queue.NewOpts) *queue.Queue {
	t.Helper()

	if opts.DB == nil {
		opts.DB = NewDB(t)
	}

	if opts.Name == "" {
		opts.Name = "test"
	}

	q, err := queue.New(opts)
	require.NoError(t, err)
	return q
}

type Logger func(msg string, args ...any)

func (f Logger) Info(msg string, args ...any) {
	f(msg, args...)
}

func NewLogger(t *testing.T) Logger {
	t.Helper()

	return Logger(func(msg string, args ...any) {
		logArgs := []any{msg}
		for i := 0; i < len(args); i += 2 {
			logArgs = append(logArgs, fmt.Sprintf("%v=%v", args[i], args[i+1]))
		}
		t.Log(logArgs...)
	})
}
