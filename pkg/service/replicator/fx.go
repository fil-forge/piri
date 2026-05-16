package replicator

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/fil-forge/go-ucanto/principal"
	logging "github.com/ipfs/go-log/v2"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/lib/jobqueue"
	"github.com/fil-forge/piri/lib/jobqueue/dialect"
	"github.com/fil-forge/piri/lib/jobqueue/serializer"
	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/service/publisher"
	replicahandler "github.com/fil-forge/piri/pkg/service/storage/handlers/replica"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/delegationstore"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

var log = logging.Logger("replicator")

var Module = fx.Module("replicator",
	fx.Provide(
		ProvideReplicationQueue,
		fx.Annotate(
			New,
			fx.As(fx.Self()),       // provide as concrete type for RegisterReplicationJobs
			fx.As(new(Replicator)), // also provide as interface
		),
	),
	fx.Invoke(RegisterReplicationJobs),
)

type QueueParams struct {
	fx.In
	DB       *sql.DB `name:"replicator_db"`
	Config   app.ReplicatorConfig
	Database app.DatabaseConfig
}

func ProvideReplicationQueue(lc fx.Lifecycle, params QueueParams) (*jobqueue.JobQueue[*replicahandler.TransferRequest], error) {
	d := dialect.SQLite
	if params.Database.IsPostgres() {
		d = dialect.Postgres
	}

	replicationQueue, err := jobqueue.New[*replicahandler.TransferRequest](
		"replication",
		params.DB,
		&serializer.JSON[*replicahandler.TransferRequest]{},
		jobqueue.WithLogger(log.With("queue", "replication")),
		jobqueue.WithMaxRetries(params.Config.MaxRetries),
		jobqueue.WithMaxWorkers(params.Config.MaxWorkers),
		jobqueue.WithMaxTimeout(params.Config.MaxTimeout),
		jobqueue.WithDialect(d),
	)
	if err != nil {
		return nil, fmt.Errorf("creating replication queue: %w", err)
	}

	queueCtx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return replicationQueue.Start(queueCtx)
		},
		OnStop: func(ctx context.Context) error {
			cancel()
			return replicationQueue.Stop(ctx)
		},
	})

	return replicationQueue, nil
}

// Params is the dependency set populated by fx for the replicator service.
type Params struct {
	fx.In

	ID          principal.Signer
	Upload      app.UploadServiceConfig
	Pieces      pdptypes.PieceAPI
	Commp       commp.Calculator
	Acceptances acceptancestore.AcceptanceStore
	ClaimStore  delegationstore.DelegationStore
	Publisher   publisher.Publisher
	Receipts    receiptstore.ReceiptStore
	Queue       *jobqueue.JobQueue[*replicahandler.TransferRequest]
}

// New constructs the replicator service.
func New(p Params) (*Service, error) {
	metrics, err := replicahandler.NewMetrics()
	if err != nil {
		return nil, err
	}
	return &Service{
		queue: p.Queue,
		deps: replicahandler.TransferDeps{
			ID:          p.ID,
			Acceptances: p.Acceptances,
			Pieces:      p.Pieces,
			Commp:       p.Commp,
			ClaimStore:  p.ClaimStore,
			Publisher:   p.Publisher,
			Receipts:    p.Receipts,
			Upload:      p.Upload,
		},
		metrics: metrics,
	}, nil
}

func RegisterReplicationJobs(
	queue *jobqueue.JobQueue[*replicahandler.TransferRequest],
	service *Service,
) error {
	return service.RegisterTransferTask(queue)
}
