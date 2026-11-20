package dedup_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/lib/jobqueue/dedup"
	internaltesting "github.com/fil-forge/piri/lib/jobqueue/internal/testing"
	"github.com/fil-forge/piri/lib/jobqueue/queue"
	"github.com/fil-forge/piri/pkg/database/sqlitedb"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	if err := internaltesting.SetupPostgresContainer(ctx); err != nil {
		// Log but continue - Postgres tests will skip
		fmt.Printf("Warning: PostgreSQL container setup failed: %v\n", err)
	}
	code := m.Run()
	internaltesting.TeardownPostgresContainer(ctx)
	os.Exit(code)
}

type envelope struct {
	Name    string `json:"Name"`
	Message []byte `json:"Message"`
}

func newTestQueueForBackend(t *testing.T, opts dedup.NewOpts, backend internaltesting.Backend) (*dedup.Queue, context.Context) {
	t.Helper()

	db := opts.DB
	if db == nil {
		if backend.IsPostgres() {
			db = internaltesting.NewPostgresDB(t)
		} else {
			var err error
			db, err = sqlitedb.NewMemory()
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = db.Close()
			})
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	// Setup schema based on backend
	if backend.IsPostgres() {
		require.NoError(t, dedup.SetupPostgres(ctx, db))
		// Clean up tables between tests (PostgreSQL shares data between tests)
		_, err := db.ExecContext(ctx, `TRUNCATE TABLE job_dead, job_done, jobs, job_ns, queues CASCADE`)
		require.NoError(t, err)
	} else {
		require.NoError(t, dedup.Setup(ctx, db))
	}

	opts.DB = db
	opts.Dialect = backend.Dialect()
	if opts.Name == "" {
		opts.Name = "test"
	}
	q, err := dedup.New(opts)
	require.NoError(t, err)
	return q, context.Background()
}

func encodeEnvelope(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	b, err := json.Marshal(envelope{Name: name, Message: payload})
	require.NoError(t, err)
	return b
}

func TestQueue_DedupSkipsCompletedPayloads(t *testing.T) {
	internaltesting.RunForAllBackends(t, func(t *testing.T, backend internaltesting.Backend) {
		q, ctx := newTestQueueForBackend(t, dedup.NewOpts{}, backend)

		body := encodeEnvelope(t, "job", []byte("payload"))
		id, err := q.SendAndGetID(ctx, queue.Message{Body: body})
		require.NoError(t, err)
		require.NotEmpty(t, id)

		msg, err := q.Receive(ctx)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, id, msg.ID)
		require.Equal(t, 1, msg.Received)

		require.NoError(t, q.Delete(ctx, msg.ID))

		dupID, err := q.SendAndGetID(ctx, queue.Message{Body: body})
		require.NoError(t, err)
		require.Empty(t, dupID, "duplicate payload should be skipped")

		next, err := q.Receive(ctx)
		require.NoError(t, err)
		require.Nil(t, next, "no job should be available after dedupe skip")
	})
}

// TestQueue_DedupSkipsPayloadCompletedDuringSend reproduces a Delete that
// commits while Send of the same payload is in flight. The test holds the
// delete transaction open so that Send passes its job_done check and then
// blocks on the conflicting live job, and commits it afterwards. Only
// Postgres runs Send concurrently with an open write transaction.
func TestQueue_DedupSkipsPayloadCompletedDuringSend(t *testing.T) {
	db := internaltesting.NewPostgresDB(t)
	q, ctx := newTestQueueForBackend(t, dedup.NewOpts{DB: db}, internaltesting.BackendPostgres)

	body := encodeEnvelope(t, "job", []byte("payload"))
	_, err := q.SendAndGetID(ctx, queue.Message{Body: body})
	require.NoError(t, err)
	msg, err := q.Receive(ctx)
	require.NoError(t, err)
	require.NotNil(t, msg)
	jobID, err := strconv.ParseInt(string(msg.ID), 10, 64)
	require.NoError(t, err)

	// Do what Delete does, without committing yet.
	deleteTx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = deleteTx.ExecContext(ctx, `INSERT INTO job_done(ns_id, key, status) SELECT ns_id, key, 1 FROM jobs WHERE id = $1`, jobID)
	require.NoError(t, err)
	_, err = deleteTx.ExecContext(ctx, `DELETE FROM jobs WHERE id = $1`, jobID)
	require.NoError(t, err)

	sendErr := make(chan error, 1)
	go func() {
		_, err := q.SendAndGetID(ctx, queue.Message{Body: body})
		sendErr <- err
	}()

	// Give Send time to pass the job_done check and block on the live job.
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, deleteTx.Commit())
	require.NoError(t, <-sendErr)

	next, err := q.Receive(ctx)
	require.NoError(t, err)
	require.Nil(t, next, "payload completed during Send must not be enqueued again")
}

func TestQueue_DedupDisabledAllowsReenqueue(t *testing.T) {
	internaltesting.RunForAllBackends(t, func(t *testing.T, backend internaltesting.Backend) {
		enabled := false
		q, ctx := newTestQueueForBackend(t, dedup.NewOpts{
			DedupeEnabled: &enabled,
		}, backend)

		body := encodeEnvelope(t, "job", []byte("payload"))
		_, err := q.SendAndGetID(ctx, queue.Message{Body: body})
		require.NoError(t, err)

		msg, err := q.Receive(ctx)
		require.NoError(t, err)
		require.NotNil(t, msg)

		require.NoError(t, q.Delete(ctx, msg.ID))

		_, err = q.SendAndGetID(ctx, queue.Message{Body: body})
		require.NoError(t, err)

		msg2, err := q.Receive(ctx)
		require.NoError(t, err)
		require.NotNil(t, msg2, "payload should be delivered again when dedupe disabled")
	})
}

func TestQueue_MoveToDeadLetterBlocksWhenConfigured(t *testing.T) {
	internaltesting.RunForAllBackends(t, func(t *testing.T, backend internaltesting.Backend) {
		q, ctx := newTestQueueForBackend(t, dedup.NewOpts{}, backend)

		body := encodeEnvelope(t, "job", []byte("payload"))
		_, err := q.SendAndGetID(ctx, queue.Message{Body: body})
		require.NoError(t, err)

		msg, err := q.Receive(ctx)
		require.NoError(t, err)
		require.NotNil(t, msg)

		require.NoError(t, q.MoveToDeadLetter(ctx, msg.ID, "job", "failed", "boom"))

		dupID, err := q.SendAndGetID(ctx, queue.Message{Body: body})
		require.NoError(t, err)
		require.Empty(t, dupID, "payload should remain blocked after DLQ move when blocking enabled")
	})
}

func TestQueue_MoveToDeadLetterAllowsReenqueueWhenBlockingDisabled(t *testing.T) {
	internaltesting.RunForAllBackends(t, func(t *testing.T, backend internaltesting.Backend) {
		block := false
		q, ctx := newTestQueueForBackend(t, dedup.NewOpts{
			BlockRepeatsOnDLQ: &block,
		}, backend)

		body := encodeEnvelope(t, "job", []byte("payload"))
		_, err := q.SendAndGetID(ctx, queue.Message{Body: body})
		require.NoError(t, err)

		msg, err := q.Receive(ctx)
		require.NoError(t, err)
		require.NotNil(t, msg)

		require.NoError(t, q.MoveToDeadLetter(ctx, msg.ID, "job", "failed", "boom"))

		_, err = q.SendAndGetID(ctx, queue.Message{Body: body})
		require.NoError(t, err)

		next, err := q.Receive(ctx)
		require.NoError(t, err)
		require.NotNil(t, next, "payload should be allowed when DLQ blocking disabled")
	})
}

func TestQueue_DuplicateWhileInFlight(t *testing.T) {
	internaltesting.RunForAllBackends(t, func(t *testing.T, backend internaltesting.Backend) {
		q, ctx := newTestQueueForBackend(t, dedup.NewOpts{}, backend)

		body := encodeEnvelope(t, "job", []byte("payload"))

		id, err := q.SendAndGetID(ctx, queue.Message{Body: body})
		require.NoError(t, err)
		require.NotEmpty(t, id)

		dupID, err := q.SendAndGetID(ctx, queue.Message{Body: body})
		require.NoError(t, err, "second enqueue of in-flight payload should not error")
		require.Empty(t, dupID, "second enqueue should be ignored while job is in-flight")

		received, err := q.Receive(ctx)
		require.NoError(t, err)
		require.NotNil(t, received)
		require.Equal(t, id, received.ID)

		next, err := q.Receive(ctx)
		require.NoError(t, err)
		require.Nil(t, next, "only one job should be present for duplicated payload")
	})
}

func TestQueue_DedupeScopedPerJobName(t *testing.T) {
	internaltesting.RunForAllBackends(t, func(t *testing.T, backend internaltesting.Backend) {
		q, ctx := newTestQueueForBackend(t, dedup.NewOpts{}, backend)

		payload := []byte("shared-payload")
		bodyA := encodeEnvelope(t, "job-a", payload)
		bodyB := encodeEnvelope(t, "job-b", payload)

		idA, err := q.SendAndGetID(ctx, queue.Message{Body: bodyA})
		require.NoError(t, err)
		require.NotEmpty(t, idA)

		idB, err := q.SendAndGetID(ctx, queue.Message{Body: bodyB})
		require.NoError(t, err)
		require.NotEmpty(t, idB, "same payload in different namespace should enqueue")

		msg1, err := q.Receive(ctx)
		require.NoError(t, err)
		require.NotNil(t, msg1)

		msg2, err := q.Receive(ctx)
		require.NoError(t, err)
		require.NotNil(t, msg2, "both namespaces should deliver a job")
		require.NotEqual(t, msg1.ID, msg2.ID)
	})
}

func TestQueue_InvalidEnvelopeRejected(t *testing.T) {
	internaltesting.RunForAllBackends(t, func(t *testing.T, backend internaltesting.Backend) {
		q, ctx := newTestQueueForBackend(t, dedup.NewOpts{}, backend)

		// Missing Name field
		badBody, err := json.Marshal(struct {
			Message []byte
		}{
			Message: []byte("payload"),
		})
		require.NoError(t, err)

		_, err = q.SendAndGetID(ctx, queue.Message{Body: badBody})
		require.Error(t, err)
		require.Contains(t, err.Error(), "message envelope missing name")

		// Not JSON at all
		_, err = q.SendAndGetID(ctx, queue.Message{Body: []byte("not-json")})
		require.Error(t, err)
		require.Contains(t, err.Error(), "decode message envelope")
	})
}
