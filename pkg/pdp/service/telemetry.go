package service

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	tracer = otel.Tracer("github.com/fil-forge/piri/pkg/pdp/service")
)

// pipelineCancelMetrics counts the pipeline rows the removal sweep cancels
// before they are folded into an aggregate, by whether the blob's commP had
// been computed, with the row's age at cancellation. A row cancelled before
// commP is a blob the commP delay saved the work for; the age histogram is
// the node's own age-at-deletion distribution, which is what sets the delay.
type pipelineCancelMetrics struct {
	cancelled metric.Int64Counter
	age       metric.Float64Histogram
}

const (
	// stageBeforeCommP labels a cancellation of a row whose commP was never
	// computed: the blob cost the pipeline nothing.
	stageBeforeCommP = "before_commp"
	// stageAfterCommP labels a cancellation of a row whose commP was
	// computed and whose piece was parked, but which was not yet aggregated.
	stageAfterCommP = "after_commp"
)

func newPipelineCancelMetrics(meter metric.Meter) (*pipelineCancelMetrics, error) {
	cancelled, err := meter.Int64Counter(
		"piri_pdp_pipeline_cancelled",
		metric.WithDescription("Pipeline rows the removal sweep cancelled before aggregation, by whether the blob's commP had been computed"),
		metric.WithUnit("{blob}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create pipeline cancelled counter: %w", err)
	}
	age, err := meter.Float64Histogram(
		"piri_pdp_pipeline_cancelled_age_seconds",
		metric.WithDescription("Age of a pipeline row, since the blob's acceptance, when the removal sweep cancelled it"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(60, 300, 900, 1800, 3600, 7200, 21600, 86400, 604800),
	)
	if err != nil {
		return nil, fmt.Errorf("create pipeline cancelled age histogram: %w", err)
	}
	return &pipelineCancelMetrics{cancelled: cancelled, age: age}, nil
}

// record counts one cancelled row. A nil receiver records nothing, so a
// service built without metrics (tests) needs no guard at the call site.
func (m *pipelineCancelMetrics) record(ctx context.Context, age time.Duration, hashed bool) {
	if m == nil {
		return
	}
	stage := stageBeforeCommP
	if hashed {
		stage = stageAfterCommP
	}
	attrs := metric.WithAttributes(attribute.String("stage", stage))
	m.cancelled.Add(ctx, 1, attrs)
	m.age.Record(ctx, age.Seconds(), attrs)
}
