package pipeline

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/fx"
)

// StartSettleMetrics exports the settle stage's backlog through the meter: how
// many accepted blobs are still staged, how long the oldest has waited, and
// how many have a settle task that gave up. A staged blob is never aggregated,
// so a backlog whose age keeps growing, or any blob whose task gave up, is how
// a failing settle shows up; the blob stays readable through the staging
// store meanwhile.
func StartSettleMetrics(ctx context.Context, meter metric.Meter, task *SettleTask) error {
	pending, err := meter.Int64ObservableGauge(
		"pdp_unsettled_blobs",
		metric.WithDescription("Accepted blobs still staged, waiting for their settle task"),
		metric.WithUnit("{blob}"),
	)
	if err != nil {
		return fmt.Errorf("create unsettled blobs gauge: %w", err)
	}
	oldest, err := meter.Float64ObservableGauge(
		"pdp_unsettled_blobs_oldest_seconds",
		metric.WithDescription("Age of the oldest accepted blob still staged"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("create oldest unsettled blob gauge: %w", err)
	}
	abandoned, err := meter.Int64ObservableGauge(
		"pdp_unsettled_blobs_abandoned",
		metric.WithDescription("Accepted blobs still staged whose settle task ran out of retries"),
		metric.WithUnit("{blob}"),
	)
	if err != nil {
		return fmt.Errorf("create abandoned unsettled blobs gauge: %w", err)
	}

	reg, err := meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			b, err := task.backlog(ctx)
			if err != nil {
				log.Warnw("measuring settle backlog", "error", err)
				return nil
			}
			o.ObserveInt64(pending, b.pending)
			o.ObserveFloat64(oldest, b.oldest.Seconds())
			o.ObserveInt64(abandoned, b.abandoned)
			return nil
		},
		pending,
		oldest,
		abandoned,
	)
	if err != nil {
		return fmt.Errorf("register settle metrics callback: %w", err)
	}

	go func() {
		<-ctx.Done()
		if err := reg.Unregister(); err != nil {
			log.Warnw("failed to unregister settle metrics callback", "error", err)
		}
	}()
	return nil
}

// registerSettleMetrics exports the settle backlog for the life of the app,
// on the global meter the serve command configures before wiring.
func registerSettleMetrics(lc fx.Lifecycle, task *SettleTask) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			meter := otel.GetMeterProvider().Meter("github.com/fil-forge/piri/pkg/pdp/pipeline")
			return StartSettleMetrics(ctx, meter, task)
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}

type settleBacklog struct {
	pending   int64
	abandoned int64
	oldest    time.Duration
}

// backlog measures the staged pipeline rows. A row whose settle task is gone
// from harmony_task while the row is still staged is one whose task ran out
// of retries: a task that succeeds clears staged before it completes.
func (t *SettleTask) backlog(ctx context.Context) (settleBacklog, error) {
	var rows []struct {
		N         int64   `db:"n"`
		Abandoned int64   `db:"abandoned"`
		Age       float64 `db:"age"`
	}
	if err := t.db.Select(ctx, &rows, `
		SELECT count(*) AS n,
		       count(*) FILTER (
		           WHERE p.settle_task_id IS NOT NULL
		             AND NOT EXISTS (SELECT 1 FROM harmony_task h WHERE h.id = p.settle_task_id)
		       ) AS abandoned,
		       coalesce(extract(epoch FROM now() - min(p.created_at)), 0)::double precision AS age
		FROM pdp_blob_pipeline p
		WHERE p.staged
	`); err != nil {
		return settleBacklog{}, fmt.Errorf("measuring settle backlog: %w", err)
	}
	if len(rows) == 0 {
		return settleBacklog{}, nil
	}
	return settleBacklog{
		pending:   rows[0].N,
		abandoned: rows[0].Abandoned,
		oldest:    time.Duration(rows[0].Age * float64(time.Second)),
	}, nil
}
