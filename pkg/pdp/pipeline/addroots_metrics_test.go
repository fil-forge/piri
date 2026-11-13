package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func newTestAddRootsTask(t *testing.T) *AddRootsTask {
	t.Helper()
	p := newTestPipeline(t)
	return NewAddRootsTask(p.db, nil, nil, nil, nil)
}

// insertPipelineRow adds a blob accepted acceptedAgo ago, folded into an
// aggregate aggregatedAgo ago, or unaggregated when aggregatedAgo is empty.
func insertPipelineRow(t *testing.T, task *AddRootsTask, seed, acceptedAgo, aggregatedAgo string) {
	t.Helper()
	blob, piece, _ := testBlobAndPiece(t, seed)
	var root, aggregatedAt any
	if aggregatedAgo != "" {
		root = "any-root"
		aggregatedAt = aggregatedAgo
	}
	_, err := task.db.Exec(t.Context(), `
		INSERT INTO pdp_blob_pipeline (digest, commp, aggregate_root, aggregated_at, created_at)
		VALUES ($1, $2, $3, now() - $4::interval, now() - $5::interval)
	`, []byte(blob), piece, root, aggregatedAt, acceptedAgo)
	require.NoError(t, err)
}

func TestAddRootsBacklog_IsEmptyForAnEmptyPipeline(t *testing.T) {
	task := newTestAddRootsTask(t)

	b, err := task.backlog(t.Context())

	require.NoError(t, err)
	require.Equal(t, addRootsBacklog{}, b)
}

func TestAddRootsBacklog_CountsOnlyAggregatedBlobs(t *testing.T) {
	task := newTestAddRootsTask(t)
	insertPipelineRow(t, task, "any-blob-1", "1 hour", "5 minutes")
	insertPipelineRow(t, task, "any-blob-2", "1 hour", "1 minute")
	insertPipelineRow(t, task, "any-blob-3", "1 hour", "") // not yet aggregated

	b, err := task.backlog(t.Context())

	require.NoError(t, err)
	require.EqualValues(t, 2, b.blobs)
}

func TestAddRootsBacklog_MeasuresTheOldestFromAggregationNotAcceptance(t *testing.T) {
	task := newTestAddRootsTask(t)
	insertPipelineRow(t, task, "any-blob-1", "3 hours", "10 minutes")
	insertPipelineRow(t, task, "any-blob-2", "3 hours", "1 minute")
	insertPipelineRow(t, task, "any-blob-3", "5 hours", "") // older, but not yet aggregated

	b, err := task.backlog(t.Context())

	require.NoError(t, err)
	require.InDelta(t, (10 * time.Minute).Seconds(), b.oldest.Seconds(), 5)
}

func TestStartAddRootsMetrics_ExportsTheBacklogUnderItsDocumentedNames(t *testing.T) {
	task := newTestAddRootsTask(t)
	insertPipelineRow(t, task, "any-blob-1", "1 hour", "10 minutes")
	insertPipelineRow(t, task, "any-blob-2", "1 hour", "1 minute")
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	require.NoError(t, StartAddRootsMetrics(t.Context(), provider.Meter("test"), task))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	got := map[string]float64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Gauge[int64]:
				got[m.Name] = float64(data.DataPoints[0].Value)
			case metricdata.Gauge[float64]:
				got[m.Name] = data.DataPoints[0].Value
			}
		}
	}
	require.Len(t, got, 2, "gauges exported: %v", got)
	require.Equal(t, float64(2), got["piri_pdp_aggregated_blobs"])
	require.InDelta(t, (10 * time.Minute).Seconds(), got["piri_pdp_aggregated_blobs_oldest_seconds"], 5)
}
