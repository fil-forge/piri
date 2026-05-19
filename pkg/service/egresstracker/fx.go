package egresstracker

// TODO(forrest)[ucan1]: do this later
/*

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"time"

	"github.com/fil-forge/ucantone/principal"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/lib/jobqueue"
	"github.com/fil-forge/piri/lib/jobqueue/dialect"
	"github.com/fil-forge/piri/lib/jobqueue/serializer"
	"github.com/fil-forge/piri/pkg/client/receipts"
	"github.com/fil-forge/piri/pkg/config/app"
	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/store/consolidationstore"
	"github.com/fil-forge/piri/pkg/store/local/retrievaljournal"
)

var log = logging.Logger("egresstracker")

var Module = fx.Module("egresstracker",
	fx.Provide(
		ProvideEgressTrackerQueue,
		ProvideReceiptsClient,
		NewEgressTrackerService,
		fx.Annotate(
			NewServer,
			fx.As(new(echofx.RouteRegistrar)),
			fx.ResultTags(`group:"route_registrar"`),
		),
	),
)

type QueueParams struct {
	fx.In

	DB            *sql.DB `name:"egress_tracker_db"`
	StorageConfig app.StorageConfig
}

func ProvideEgressTrackerQueue(lc fx.Lifecycle, params QueueParams) (EgressTrackerQueue, error) {
	// Determine dialect from storage config
	d := dialect.SQLite
	if params.StorageConfig.Database.IsPostgres() {
		d = dialect.Postgres
	}
	// non-configurable defaults
	maxRetries := uint(10)
	maxWorkers := uint(runtime.NumCPU())
	maxTimeout := 5 * time.Second

	queue, err := jobqueue.New(
		"egress-tracker",
		params.DB,
		&serializer.JSON[cid.Cid]{},
		jobqueue.WithLogger(log.With("queue", "egress-tracker")),
		jobqueue.WithMaxRetries(maxRetries),
		jobqueue.WithMaxWorkers(maxWorkers),
		jobqueue.WithMaxTimeout(maxTimeout),
		jobqueue.WithDialect(d),
	)
	if err != nil {
		return nil, fmt.Errorf("creating egress-tracker queue: %w", err)
	}

	queueCtx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return queue.Start(queueCtx)
		},
		OnStop: func(ctx context.Context) error {
			cancel()               // Cancel the Start context first
			return queue.Stop(ctx) // Then wait for graceful shutdown
		},
	})

	return NewEgressTrackerQueue(queue), nil
}

func ProvideReceiptsClient(cfg app.EgressTrackerServiceConfig) *receipts.Client {
	return receipts.NewClient(cfg.ReceiptsEndpoint)
}

func NewEgressTrackerService(
	lc fx.Lifecycle,
	id principal.Signer,
	journal retrievaljournal.Journal,
	consolidationStore consolidationstore.Store,
	queue EgressTrackerQueue,
	rcptsClient *receipts.Client,
	serverCfg app.ServerConfig,
	cfg app.EgressTrackerServiceConfig,
) (*Service, error) {
	if cfg.Connection == nil {
		log.Warn("no egress tracker service connection provided, egress tracking is disabled")
		return nil, nil
	}

	cleanupCheckInterval := cfg.CleanupCheckInterval
	// Disable cleanup if receipts endpoint is not configured or empty
	if cfg.ReceiptsEndpoint == nil || cfg.ReceiptsEndpoint.String() == "" {
		log.Warn("no egress tracker receipts endpoint configured, cleanup task will be disabled")
		cleanupCheckInterval = 0 // Disable cleanup
	}

	svc, err := New(
		id,
		cfg.Connection,
		cfg.Proofs,
		serverCfg.PublicURL.JoinPath(ReceiptsPath+"/{cid}"),
		journal,
		consolidationStore,
		queue,
		rcptsClient,
		cleanupCheckInterval,
	)
	if err != nil {
		return nil, err
	}

	// Add lifecycle hooks for start and stop
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			return svc.Start(ctx)
		},
		OnStop: func(ctx context.Context) error {
			cancel()
			return svc.Stop(ctx)
		},
	})

	return svc, nil
}

*/
