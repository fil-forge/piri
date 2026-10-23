package publisher

import (
	"context"
	"fmt"

	"github.com/fil-forge/go-ipni-tools/pkg/store"
	"github.com/fil-forge/libforge/identity"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	pstore "github.com/fil-forge/piri/pkg/store"
)

var Module = fx.Module("publisher",
	fx.Provide(
		fx.Annotate(
			NewFx,
			fx.As(fx.Self()),
			fx.As(new(Publisher)),
			fx.As(new(Withdrawer)),
		),
		fx.Annotate(
			NewServer,
			fx.As(new(echofx.RouteRegistrar)),
			fx.ResultTags(`group:"route_registrar"`),
		),
	),
)

// taskGroup collects every harmonytask.TaskInterface the curiopdp engine
// runs; it must match the group consumed in pkg/curiopdp/module.go.
const taskGroup = `group:"curio_harmonytasks"`

// QueueModule wires the harmonydb advertisement queue and the task that
// drains it. It is composed into the full server beside Module rather than
// inside it: the task needs the PDP modules' harmonydb, and a composition
// without one (the ucanfxtest suites) supplies an in-memory AdvertQueue.
var QueueModule = fx.Module("publisher/queue",
	fx.Provide(
		fx.Annotate(
			NewDBQueue,
			fx.As(fx.Self()),
			fx.As(new(AdvertQueue)),
		),
		NewPublishTask,
		fx.Annotate(asTask[*PublishTask], fx.ResultTags(taskGroup)),
	),
	fx.Invoke(registerQueueMetrics, startPublishTicker),
)

// startPublishTicker runs the publish task's flush ticker for the life of
// the app.
func startPublishTicker(lc fx.Lifecycle, task *PublishTask) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go task.run(ctx)
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}

func asTask[T harmonytask.TaskInterface](t T) harmonytask.TaskInterface { return t }

// FxParams are the publisher service's dependencies. BatchStore is provided
// only by a store that publishes a batch in one transaction.
type FxParams struct {
	fx.In
	PubCfg         app.PublisherServiceConfig
	IdxCfg         app.IndexingServiceConfig
	ID             identity.Identity
	PublisherStore store.PublisherStore
	BatchStore     store.BatchStore `optional:"true"`
	Queue          AdvertQueue
	Tx             pstore.Transactor
}

// NewFx wires the publisher service from its narrow configs.
func NewFx(p FxParams) (*PublisherService, error) {
	if p.PubCfg.PublicMaddr.String() == "" {
		return nil, fmt.Errorf("public address is required for publisher service")
	}

	opts := []Option{
		WithDirectAnnounce(p.PubCfg.AnnounceURLs...),
		WithIndexingService(p.IdxCfg),
		WithIndexingServiceProof(p.IdxCfg.Proofs),
		WithAnnounceAddress(p.PubCfg.AnnounceMaddr),
		WithBlobAddress(p.PubCfg.BlobMaddr),
		WithTransactor(p.Tx),
	}
	if p.BatchStore != nil {
		opts = append(opts, WithBatchStore(p.BatchStore))
	}
	return New(p.ID, p.PublisherStore, p.PubCfg.PublicMaddr, p.Queue, opts...)
}
