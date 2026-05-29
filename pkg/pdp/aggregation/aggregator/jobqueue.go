package aggregator

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"slices"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	logging "github.com/ipfs/go-log/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	libpiece "github.com/fil-forge/libforge/piece"
	"github.com/fil-forge/piri/lib/jobqueue"
	"github.com/fil-forge/piri/lib/jobqueue/dialect"
	"github.com/fil-forge/piri/lib/jobqueue/serializer"
	"github.com/fil-forge/piri/lib/jobqueue/traceutil"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/manager"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/types"
)

var log = logging.Logger("aggregation/aggregator")

type AggregatorParams struct {
	fx.In
	Queue   jobqueue.Service[types.AggregatorJob]
	Handler jobqueue.TaskHandler[types.AggregatorJob]
}

type Aggregator struct {
	queue   jobqueue.Service[types.AggregatorJob]
	handler jobqueue.TaskHandler[types.AggregatorJob]
}

func New(lc fx.Lifecycle, params AggregatorParams) (*Aggregator, error) {
	a := &Aggregator{
		queue:   params.Queue,
		handler: params.Handler,
	}

	if err := a.queue.RegisterHandler(params.Handler); err != nil {
		return nil, fmt.Errorf("registering aggregator handler: %w", err)
	}

	queueCtx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return a.queue.Start(queueCtx)
		},
		OnStop: func(ctx context.Context) error {
			cancel()
			return a.queue.Stop(ctx)
		},
	})

	return a, nil
}

func (a *Aggregator) EnqueueAggregation(ctx context.Context, p libpiece.Piece) error {
	log.Infow("enqueuing piece for aggregation", "piece", p.CID())
	return a.queue.Enqueue(ctx, a.handler.Name(), types.AggregatorJob{Piece: p.CID()})
}

const (
	QueueName = "aggregator"
	TaskName  = "aggregate_piece"
)

type QueueParams struct {
	fx.In
	DB            *sql.DB `name:"aggregator_db"`
	StorageConfig app.StorageConfig
}

func NewQueue(params QueueParams) (jobqueue.Service[types.AggregatorJob], error) {
	// Determine dialect from storage config
	d := dialect.SQLite
	if params.StorageConfig.Database.IsPostgres() {
		d = dialect.Postgres
	}

	// The deduping is required to ensure we don't produce an aggregate with sub roots that exist in another aggregate
	// the behavior here is to ignore duplicate pieces we have already aggregated
	// this is required to ensure roots are added with distinct sub roots from existing roots.
	// While the chain logic permits this, it can lead to duplicate data being proved and thus paid for.
	// Do not allow successfully complete jobs to run again.
	dedupEnabled := true
	// Allow jobs in dead letter queue (failed) to run again.
	blockDLQRetries := false
	linkQueue, err := jobqueue.New[types.AggregatorJob](
		QueueName,
		params.DB,
		serializer.CBOR[types.AggregatorJob]{},
		jobqueue.WithLogger(log.With("queue", QueueName)),
		jobqueue.WithMaxRetries(50),
		// one worker to keep things serial
		jobqueue.WithMaxWorkers(uint(runtime.NumCPU())),
		// one filecoin epoch since this is wrongly running tasks, we need yet another queue.....
		jobqueue.WithMaxTimeout(30*time.Second),
		jobqueue.WithDialect(d),
		// we enable de-duplication for this queue since we only want to aggregate a piece once.
		jobqueue.WithDedupQueue(&jobqueue.DedupQueueConfig{
			DedupeEnabled:     &dedupEnabled,
			BlockRepeatsOnDLQ: &blockDLQRetries,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("creating aggregator job-queue: %w", err)
	}
	return linkQueue, nil
}

type HandlerParams struct {
	fx.In
	Store     types.Store
	Datastore datastore.Datastore `name:"aggregator_datastore"`
	Manager   *manager.Manager
}

func NewHandler(params HandlerParams) jobqueue.TaskHandler[types.AggregatorJob] {
	return &Handler{
		workspace: newInProgressWorkspace(params.Datastore),
		store:     params.Store,
		manager:   params.Manager,
	}
}

type Handler struct {
	workspace InProgressWorkspace
	store     types.Store
	manager   *manager.Manager
}

func (p *Handler) Handle(ctx context.Context, job types.AggregatorJob) (retErr error) {
	piece, err := libpiece.FromCID(job.Piece)
	if err != nil {
		return fmt.Errorf("decoding piece from cid %s: %w", job.Piece, err)
	}

	ctx, span := traceutil.StartSpan(ctx, tracer, "aggregator.Handle", trace.WithAttributes(attribute.String("piece", piece.CID().String())))
	defer func() {
		if retErr != nil {
			span.RecordError(retErr)
			span.SetStatus(codes.Error, "failed to aggregate piece")
		}
		span.End()
	}()

	log.Infow("aggregating piece", "link", piece.CID())
	buffer, err := p.workspace.GetBuffer(ctx)
	if err != nil {
		return fmt.Errorf("reading in progress pieces from work space: %w", err)
	}
	buffer, a, err := AggregatePiece(buffer, piece)
	if err != nil {
		return fmt.Errorf("calculating aggegates: %w", err)
	}
	if err := p.workspace.PutBuffer(ctx, buffer); err != nil {
		return fmt.Errorf("updating work space: %w", err)
	}
	if a != nil {
		span.AddEvent("aggregate created", trace.WithAttributes(attribute.String("aggregate.root", a.Root.String())))
		if err := p.store.Put(ctx, a.Root, *a); err != nil {
			return fmt.Errorf("storing aggregate: %w", err)
		}
		if err := p.manager.Submit(ctx, a.Root); err != nil {
			return fmt.Errorf("submitting aggregate to manager: %w", err)
		}
	}
	return nil
}

func (p *Handler) Name() string {
	return TaskName
}

// MinAggregateSize is 128MB
// Max size is 256MB -- this means we will never see an individual piece larger
// than 256MB -- the upload will fail otherwise
// So we can safely assume that if we see a 256MB piece, we just submit immediately
// If not, we can safely aggregate till >=128MB without going over 256MB
const MinAggregateSize = 128 << 20

// AggregatePiece appends newPiece to buffer; when the running size reaches the
// minimum threshold it produces an aggregate and resets the buffer.
//
// The in-memory aggregation logic operates on piri_piece.Piece (which carries
// PaddedSize() and other methods); only the Buffer's serialized form uses
// cid.Cid. The function converts at the boundary.
func AggregatePiece(buffer types.Buffer, newPiece libpiece.Piece) (types.Buffer, *types.Aggregate, error) {
	log.Infow("aggregating piece",
		"link", newPiece.CID().String(),
		"padded size", newPiece.PaddedSize(),
		"buffer size", buffer.TotalSize,
	)
	// if the piece is aggregatable on its own it should submit immediately
	if newPiece.PaddedSize() > MinAggregateSize {
		aggregate, err := NewAggregate([]libpiece.Piece{newPiece})
		if err == nil {
			log.Infow("aggregate create", "root", aggregate.Root)
		}
		return buffer, &aggregate, err
	}

	bufferPieces, err := decodePieces(buffer.ReverseSortedPieces)
	if err != nil {
		return buffer, nil, fmt.Errorf("decoding buffered pieces: %w", err)
	}
	newSize := buffer.TotalSize + newPiece.PaddedSize()
	newPieces := InsertOrderedByDescendingSize(bufferPieces, newPiece)

	// if we have reached the minimum aggregate size, submit and start over
	if newSize >= MinAggregateSize {
		aggregate, err := NewAggregate(newPieces)
		if err != nil {
			return buffer, nil, err
		}
		log.Infow("aggregate create", "root", aggregate.Root)
		return types.Buffer{}, &aggregate, err
	}

	// otherwise keep aggregating
	return types.Buffer{
		TotalSize:           newSize,
		ReverseSortedPieces: encodePieces(newPieces),
	}, nil, nil
}

func AggregatePieces(buffer types.Buffer, pieces []libpiece.Piece) (types.Buffer, []types.Aggregate, error) {
	var aggregates []types.Aggregate
	for _, p := range pieces {
		var aggregate *types.Aggregate
		var err error
		buffer, aggregate, err = AggregatePiece(buffer, p)
		if err != nil {
			return buffer, aggregates, err
		}
		if aggregate != nil {
			aggregates = append(aggregates, *aggregate)
		}
	}
	return buffer, aggregates, nil
}

// InsertOrderedByDescendingSize adds a piece to a list of pieces sorted
// largest to smallest, maintaining sort order.
func InsertOrderedByDescendingSize(sortedPieces []libpiece.Piece, newPiece libpiece.Piece) []libpiece.Piece {
	pos, _ := slices.BinarySearchFunc(sortedPieces, newPiece, func(test, target libpiece.Piece) int {
		// flip ordering comparing size cause we're going in reverse order
		return cmp.Compare(target.PaddedSize(), test.PaddedSize())
	})
	return slices.Insert(sortedPieces, pos, newPiece)
}

// decodePieces rehydrates a buffer's persisted CID slice into the
// [libpiece.Piece] form aggregation operates on internally.
func decodePieces(cids []cid.Cid) ([]libpiece.Piece, error) {
	if len(cids) == 0 {
		return nil, nil
	}
	out := make([]libpiece.Piece, len(cids))
	for i, c := range cids {
		p, err := libpiece.FromCID(c)
		if err != nil {
			return nil, fmt.Errorf("decoding piece %s: %w", c, err)
		}
		out[i] = p
	}
	return out, nil
}

func encodePieces(pieces []libpiece.Piece) []cid.Cid {
	if len(pieces) == 0 {
		return nil
	}
	out := make([]cid.Cid, len(pieces))
	for i, p := range pieces {
		out[i] = p.CID()
	}
	return out
}
