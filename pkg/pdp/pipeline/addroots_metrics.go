package pipeline

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/fx"
)

// StartAddRootsMetrics exports the AddRoots stage's backlog through the
// meter: how many blobs are folded into an aggregate whose root has not been
// added on chain, and how long ago the oldest of them was folded. AddRoots
// sends a full batch of roots at once and a partial one on the next poll
// interval, then waits for the transaction to confirm before it retires the
// rows, so a healthy oldest age stays under about 5 minutes. An age that
// keeps growing means AddRoots has stopped retiring aggregates, or sends them
// slower than they are folded.
func StartAddRootsMetrics(ctx context.Context, meter metric.Meter, task *AddRootsTask) error {
	blobs, err := meter.Int64ObservableGauge(
		"piri_pdp_aggregated_blobs",
		metric.WithDescription("Blobs folded into an aggregate whose root has not been added on chain"),
		metric.WithUnit("{blob}"),
	)
	if err != nil {
		return fmt.Errorf("create aggregated blobs gauge: %w", err)
	}
	oldest, err := meter.Float64ObservableGauge(
		"piri_pdp_aggregated_blobs_oldest_seconds",
		metric.WithDescription("How long ago the oldest blob not yet added on chain was folded into its aggregate"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("create oldest aggregated blob gauge: %w", err)
	}

	reg, err := meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			b, err := task.backlog(ctx)
			if err != nil {
				log.Warnw("measuring addroots backlog", "error", err)
				return nil
			}
			o.ObserveInt64(blobs, b.blobs)
			o.ObserveFloat64(oldest, b.oldest.Seconds())
			return nil
		},
		blobs,
		oldest,
	)
	if err != nil {
		return fmt.Errorf("register addroots metrics callback: %w", err)
	}

	go func() {
		<-ctx.Done()
		if err := reg.Unregister(); err != nil {
			log.Warnw("failed to unregister addroots metrics callback", "error", err)
		}
	}()
	return nil
}

// registerAddRootsMetrics exports the AddRoots backlog for the life of the
// app, on the global meter the serve command configures before wiring.
func registerAddRootsMetrics(lc fx.Lifecycle, task *AddRootsTask) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			meter := otel.GetMeterProvider().Meter("github.com/fil-forge/piri/pkg/pdp/pipeline")
			return StartAddRootsMetrics(ctx, meter, task)
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}

type addRootsBacklog struct {
	blobs  int64
	oldest time.Duration
}

// backlog measures the aggregated pipeline rows. AddRoots deletes a batch's
// rows once its roots are added on chain, so every row with an aggregate root
// is still waiting for that.
func (t *AddRootsTask) backlog(ctx context.Context) (addRootsBacklog, error) {
	var rows []struct {
		N      int64   `db:"n"`
		Oldest float64 `db:"oldest"`
	}
	if err := t.db.Select(ctx, &rows, `
		SELECT count(*) AS n,
		       coalesce(extract(epoch FROM now() - min(aggregated_at)), 0)::double precision AS oldest
		FROM pdp_blob_pipeline
		WHERE aggregate_root IS NOT NULL
	`); err != nil {
		return addRootsBacklog{}, fmt.Errorf("measuring addroots backlog: %w", err)
	}
	if len(rows) == 0 {
		return addRootsBacklog{}, nil
	}
	return addRootsBacklog{
		blobs:  rows[0].N,
		oldest: time.Duration(rows[0].Oldest * float64(time.Second)),
	}, nil
}
