package pipeline

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/fx"
)

// StartCommPMetrics exports the commP stage's entry queue through the meter:
// how many accepted blobs are waiting out the delay, how many are due with no
// task yet, and how long the oldest of those has been due. Held blobs are the
// delay working as configured. Due blobs should be claimed within a poll of
// the task engine, so a due count that grows, or an oldest age past a few
// minutes, means claims have stopped: the engine is down, or every poll finds
// other work first.
func StartCommPMetrics(ctx context.Context, meter metric.Meter, task *CommPTask) error {
	held, err := meter.Int64ObservableGauge(
		"piri_pdp_commp_held_blobs",
		metric.WithDescription("Accepted blobs waiting out the commP delay before a commP task may claim them"),
		metric.WithUnit("{blob}"),
	)
	if err != nil {
		return fmt.Errorf("create held blobs gauge: %w", err)
	}
	due, err := meter.Int64ObservableGauge(
		"piri_pdp_commp_due_blobs",
		metric.WithDescription("Accepted blobs past the commP delay that no commP task has claimed"),
		metric.WithUnit("{blob}"),
	)
	if err != nil {
		return fmt.Errorf("create due blobs gauge: %w", err)
	}
	oldest, err := meter.Float64ObservableGauge(
		"piri_pdp_commp_due_blobs_oldest_seconds",
		metric.WithDescription("How long the oldest unclaimed blob has been past the commP delay"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("create oldest due blob gauge: %w", err)
	}

	reg, err := meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			b, err := task.backlog(ctx)
			if err != nil {
				log.Warnw("measuring commp backlog", "error", err)
				return nil
			}
			o.ObserveInt64(held, b.held)
			o.ObserveInt64(due, b.due)
			o.ObserveFloat64(oldest, b.oldestDue.Seconds())
			return nil
		},
		held,
		due,
		oldest,
	)
	if err != nil {
		return fmt.Errorf("register commp metrics callback: %w", err)
	}

	go func() {
		<-ctx.Done()
		if err := reg.Unregister(); err != nil {
			log.Warnw("failed to unregister commp metrics callback", "error", err)
		}
	}()
	return nil
}

// registerCommPMetrics exports the commP entry queue for the life of the
// app, on the global meter the serve command configures before wiring.
func registerCommPMetrics(lc fx.Lifecycle, task *CommPTask) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			meter := otel.GetMeterProvider().Meter("github.com/fil-forge/piri/pkg/pdp/pipeline")
			return StartCommPMetrics(ctx, meter, task)
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}

type commpBacklog struct {
	held      int64
	due       int64
	oldestDue time.Duration
}

// backlog measures the unclaimed, settled pipeline rows against the current
// delay: those still held by it and those due, with how long the oldest due
// row has waited past its due time. A row whose blob has a removal pending
// is left out, as the claims leave it out: it is the sweep's to cancel, and
// counting it as due would show claims stalled while they are working.
func (t *CommPTask) backlog(ctx context.Context) (commpBacklog, error) {
	var rows []struct {
		Held   int64   `db:"held"`
		Due    int64   `db:"due"`
		Oldest float64 `db:"oldest"`
	}
	if err := t.db.Select(ctx, &rows, `
		WITH unclaimed AS (
		    SELECT created_at,
		           created_at + make_interval(secs => $1::double precision) AS due_at
		    FROM pdp_blob_pipeline
		    WHERE commp_task_id IS NULL AND aggregate_root IS NULL AND NOT staged
		      AND NOT EXISTS (SELECT 1 FROM pdp_pending_piece_removals r WHERE r.digest = pdp_blob_pipeline.digest)
		)
		SELECT count(*) FILTER (WHERE due_at > now()) AS held,
		       count(*) FILTER (WHERE due_at <= now()) AS due,
		       coalesce(extract(epoch FROM now() - min(due_at) FILTER (WHERE due_at <= now())), 0)::double precision AS oldest
		FROM unclaimed
	`, t.delaySeconds()); err != nil {
		return commpBacklog{}, fmt.Errorf("measuring commp backlog: %w", err)
	}
	if len(rows) == 0 {
		return commpBacklog{}, nil
	}
	return commpBacklog{
		held:      rows[0].Held,
		due:       rows[0].Due,
		oldestDue: time.Duration(rows[0].Oldest * float64(time.Second)),
	}, nil
}
