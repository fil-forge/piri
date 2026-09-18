package publisher

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/fx"
)

// StartQueueMetrics exports the advertisement queue's backlog through the
// meter: how many advertisements are queued and not yet published, claimed by
// a task or not, and how long the oldest has waited. Once publishing is
// asynchronous the accept receipt can no longer report a failure to
// advertise; a backlog that keeps growing, or an age that does, is how a
// publisher that is down or failing shows up.
func StartQueueMetrics(ctx context.Context, meter metric.Meter, queue *DBQueue) error {
	pending, err := meter.Int64ObservableGauge(
		"ipni_pending_adverts",
		metric.WithDescription("IPNI advertisements queued and not yet published"),
		metric.WithUnit("{advertisement}"),
	)
	if err != nil {
		return fmt.Errorf("create pending adverts gauge: %w", err)
	}
	oldest, err := meter.Float64ObservableGauge(
		"ipni_pending_adverts_oldest_seconds",
		metric.WithDescription("Age of the oldest IPNI advertisement queued and not yet published"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("create oldest pending advert gauge: %w", err)
	}

	reg, err := meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			count, age, err := queue.pending(ctx)
			if err != nil {
				log.Warnw("measuring advertisement queue", "error", err)
				return nil
			}
			o.ObserveInt64(pending, count)
			o.ObserveFloat64(oldest, age.Seconds())
			return nil
		},
		pending,
		oldest,
	)
	if err != nil {
		return fmt.Errorf("register advertisement queue metrics callback: %w", err)
	}

	go func() {
		<-ctx.Done()
		if err := reg.Unregister(); err != nil {
			log.Warnw("failed to unregister advertisement queue metrics callback", "error", err)
		}
	}()
	return nil
}

// registerQueueMetrics exports the queue's backlog for the life of the app,
// on the global meter the serve command configures before wiring.
func registerQueueMetrics(lc fx.Lifecycle, queue *DBQueue) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			meter := otel.GetMeterProvider().Meter("github.com/fil-forge/piri/pkg/service/publisher")
			return StartQueueMetrics(ctx, meter, queue)
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}
