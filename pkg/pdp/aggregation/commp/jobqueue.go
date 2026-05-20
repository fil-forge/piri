package commp

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"

	"github.com/multiformats/go-multihash"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/lib/jobqueue"
	"github.com/fil-forge/piri/lib/jobqueue/dialect"
	"github.com/fil-forge/piri/lib/jobqueue/serializer"
	"github.com/fil-forge/piri/lib/jobqueue/traceutil"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/aggregator"
	aggtypes "github.com/fil-forge/piri/pkg/pdp/aggregation/types"
	piri_piece "github.com/fil-forge/piri/pkg/pdp/piece"
	"github.com/fil-forge/piri/pkg/pdp/types"
)

type CommpQueueParams struct {
	fx.In
	DB            *sql.DB `name:"aggregator_db"`
	StorageConfig app.StorageConfig
}

const (
	QueueName = "commp"
	TaskName  = "compute_commp"
)

func NewQueue(params CommpQueueParams) (jobqueue.Service[aggtypes.CommpJob], error) {
	// Determine dialect from storage config
	d := dialect.SQLite
	if params.StorageConfig.Database.IsPostgres() {
		d = dialect.Postgres
	}

	commpQueue, err := jobqueue.New[aggtypes.CommpJob](
		TaskName,
		params.DB,
		serializer.CBOR[aggtypes.CommpJob]{},
		jobqueue.WithLogger(log.With("queue", QueueName)),
		// TODO(forrest) make these configuration parameters.
		jobqueue.WithMaxRetries(50),
		jobqueue.WithMaxWorkers(uint(runtime.NumCPU())),
		jobqueue.WithDialect(d),
	)
	if err != nil {
		return nil, fmt.Errorf("creating commp queue: %w", err)
	}

	return commpQueue, nil
}

func NewHandler(api types.PieceAPI, a *aggregator.Aggregator) jobqueue.TaskHandler[aggtypes.CommpJob] {
	return &ComperTaskHandler{api: api, aggregator: a}
}

type ComperTaskHandler struct {
	api        types.PieceAPI
	aggregator *aggregator.Aggregator
}

func (h *ComperTaskHandler) Handle(ctx context.Context, job aggtypes.CommpJob) error {
	blob := multihash.Multihash(job.Digest)

	ctx, span := traceutil.StartSpan(ctx, tracer, "commp.Handle", trace.WithAttributes(attribute.Stringer("blob.digest", blob)))
	defer span.End()

	res, err := h.api.CalculateCommP(ctx, blob)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to calculate commp")
		return fmt.Errorf("calculating commp: %w", err)
	}
	span.AddEvent("calculated commp")

	log.Infow("calculated commp", "blob", blob.String(), "piece", res.PieceCID.Hash().String(), "link", res.PieceCID.String())
	if err := h.api.ParkPiece(ctx, types.ParkPieceRequest{
		Blob:       blob,
		PieceCID:   res.PieceCID,
		RawSize:    res.RawSize,
		PaddedSize: res.PaddedSize,
	}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to park piece")
		return fmt.Errorf("parking piece: %w", err)
	}
	span.AddEvent("parked piece")

	p, err := piri_piece.FromCID(res.PieceCID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to convert piece")
		return err
	}
	span.SetAttributes([]attribute.KeyValue{
		attribute.Int64("blob.size", res.RawSize),
		attribute.Stringer("piece", res.PieceCID),
		attribute.Stringer("piece.digest", res.PieceCID.Hash()),
		attribute.Int64("piece.padded_size", res.PaddedSize),
	}...)
	return h.aggregator.EnqueueAggregation(ctx, p)
}

func (h *ComperTaskHandler) Name() string {
	return TaskName
}
