package curiopdp

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/fx"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/curio/tasks/pay"
	"github.com/filecoin-project/curio/tasks/pdpv0"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/pdp/service"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store/blobstore"
)

// taskGroup is the fx group collecting every harmonytask.TaskInterface the engine runs.
const taskGroup = `group:"curio_harmonytasks"`

// harmonyMachineID identifies this node in harmony_machines.
// TODO(e2e): wire to Piri's real listen address; must be unique per node.
const harmonyMachineID = "127.0.0.1:12300"

// Module wires Curio's pdpv0 PDP pipeline (harmonytask scheduler + init/next
// proving-period + prove tasks) into Piri, backed by Piri's S3 blobstore, eth
// client, and Lotus node. Requires Postgres (see ProvideHarmonyDB).
//
// Consumes from Piri's existing graph: *ethclient.Client, api.FullNode,
// blobstore.Blobstore, types.PieceResolverAPI, app.StorageConfig.
//
// Deferred to e2e/devnet: provisioning the signing key into eth_keys, a real
// machine identity, and validating the watcher trigger chain end-to-end.
var Module = fx.Module("curio-pdp",
	fx.Provide(
		ProvideHarmonyDB,
		provideEthClient,
		provideChainSched,
		provideAlerting,
		provideWatcher,
		provideNextPPChainAPI,
		provideProveChainAPI,
		provideSenderBundle,
		provideSender,
		provideS3PieceReader,
		provideProofCache,

		// Tasks — each collected into the engine's task group.
		fx.Annotate(provideSendTask, fx.ResultTags(taskGroup)),
		fx.Annotate(provideInitTask, fx.ResultTags(taskGroup)),
		fx.Annotate(provideNextTask, fx.ResultTags(taskGroup)),
		fx.Annotate(provideProveTask, fx.ResultTags(taskGroup)),
		fx.Annotate(provideSettleTask, fx.ResultTags(taskGroup)),

		// Engine consumes the whole task group.
		fx.Annotate(provideEngine, fx.ParamTags("", "", taskGroup)),
	),
	fx.Invoke(provisionPDPState),
	fx.Invoke(startPipeline),
)

// SetContractAddresses installs Piri's configured PDP contract addresses into
// Curio's contract package. Call it once at startup, before building the fx app —
// the pdpv0 task constructors resolve addresses eagerly during fx construction.
// Curio then resolves addresses from Piri's config instead of its network build
// tag / CURIO_DEVNET_* env vars.
func SetContractAddresses(cfg app.PDPServiceConfig) {
	contract.SetAddresses(contract.Addresses{
		PDPVerifier:     cfg.Contracts.Verifier,
		FWSService:      cfg.Contracts.Service,
		ServiceRegistry: cfg.Contracts.ProviderRegistry,
		USDFC:           cfg.Contracts.USDFCToken,
	})
}

// --- external-dependency adapters (Curio concrete types over Piri's services) ---

// provideEthClient: Piri's go-ethereum client satisfies Curio's EthClient interface.
func provideEthClient(c *ethclient.Client) ethchain.EthClient { return c }

// provideChainSched: Curio's chain scheduler over Piri's Lotus node. Piri exposes
// its Lotus client as service.ChainClient (not raw api.FullNode), which carries
// ChainHead+ChainNotify (satisfies chainsched.NodeAPI) plus the randomness call.
func provideChainSched(n service.ChainClient) *chainsched.CurioChainSched { return chainsched.New(n) }

// provideAlerting: Piri has no alerting subsystem, so PDP components that require a
// curioalerting.AlertingInterface get a no-op sink.
func provideAlerting() curioalerting.AlertingInterface { return noopAlerting{} }

// provideWatcher: the async ordered PDPv0 chain watcher. It registers a handler on
// the chain scheduler at construction; the pdpv0 tasks register their per-tipset
// callbacks on it, and startPipeline runs it once all callbacks are registered.
func provideWatcher(db *harmonydb.DB, ec ethchain.EthClient, cs *chainsched.CurioChainSched, al curioalerting.AlertingInterface) *pdpv0.Watcher {
	return pdpv0.NewPDPv0Watcher(db, ec, cs, al)
}

// The proving tasks' chain APIs are satisfied by service.ChainClient too.
func provideNextPPChainAPI(n service.ChainClient) pdpv0.NextProvingPeriodTaskChainApi { return n }
func provideProveChainAPI(n service.ChainClient) pdpv0.ProveTaskChainApi              { return n }

// senderBundle keeps the (SenderETH, SendTaskETH) pair together.
type senderBundle struct {
	sender   *message.SenderETH
	sendTask *message.SendTaskETH
}

func provideSenderBundle(c ethchain.EthClient, db *harmonydb.DB) *senderBundle {
	s, st := message.NewSenderETH(c, db)
	return &senderBundle{sender: s, sendTask: st}
}

func provideSender(b *senderBundle) *message.SenderETH          { return b.sender }
func provideSendTask(b *senderBundle) harmonytask.TaskInterface { return b.sendTask }

func provideS3PieceReader(store blobstore.Blobstore, resolver types.PieceResolverAPI) pdpv0.PieceReader {
	return NewS3PieceReader(store, resolver)
}

// provideProofCache returns a nil store: Piri has no proof cache yet, and the
// prove task gates its cached-proof branch on `p.idx != nil` alone. A non-nil
// no-op store therefore reads as "cache available" — for every sub-piece above
// MinSizeForCache (32 MiB padded) the task would call GenerateCachedProof, get
// (nil, nil) back, treat that as a cache failure, and write
// `needs_save_cache = TRUE, cached_proofgen_failure_count = n+1` to
// pdp_piecerefs on *every* proof, with no SaveCache task registered to drain
// it. Returning nil skips the branch entirely and goes straight to the
// full-memtree path, which is what Piri actually proves with today.
//
// Replace this with a real ProofCacheStore (and register pdpv0's SaveCache
// task) to pick up the proving optimization.
func provideProofCache() pdpv0.ProofCacheStore { return nil }

// --- pdpv0 pipeline tasks ---

func provideInitTask(db *harmonydb.DB, ec ethchain.EthClient, fil pdpv0.NextProvingPeriodTaskChainApi, w *pdpv0.Watcher, s *message.SenderETH) harmonytask.TaskInterface {
	return pdpv0.NewInitProvingPeriodTask(db, ec, fil, w, s)
}

func provideNextTask(db *harmonydb.DB, ec ethchain.EthClient, fil pdpv0.NextProvingPeriodTaskChainApi, w *pdpv0.Watcher, s *message.SenderETH) harmonytask.TaskInterface {
	return pdpv0.NewNextProvingPeriodTask(db, ec, fil, w, s)
}

func provideProveTask(db *harmonydb.DB, ec ethchain.EthClient, fil pdpv0.ProveTaskChainApi, w *pdpv0.Watcher, s *message.SenderETH, pr pdpv0.PieceReader, pc pdpv0.ProofCacheStore) harmonytask.TaskInterface {
	return pdpv0.NewProveTask(db, ec, fil, w, s, pr, pc)
}

// provideSettleTask: Curio's autonomous payment-rail settlement task (periodic singleton).
func provideSettleTask(db *harmonydb.DB, ec ethchain.EthClient, s *message.SenderETH, al curioalerting.AlertingInterface) harmonytask.TaskInterface {
	return pay.NewSettleTask(db, ec, s, al)
}

// --- engine + pipeline startup ---

// resourceInspector reports machine resources for the harmonytask scheduler. Piri
// never has a GPU, so it reports zero GPUs; CPU and RAM come from resources.System
// (the FFI-free probe) so the scheduler sizes work correctly.
type resourceInspector struct{}

func (resourceInspector) GetResources() (resources.Resources, error) { return resources.System(0) }

func provideEngine(lc fx.Lifecycle, db *harmonydb.DB, tasks []harmonytask.TaskInterface) (*harmonytask.TaskEngine, error) {
	eng, err := harmonytask.New(db, tasks, harmonyMachineID, resourceInspector{})
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error {
		eng.GracefullyTerminate()
		return nil
	}})
	return eng, nil
}

// startPipeline registers the chain watchers that drive the pipeline and starts the
// chain scheduler and the pdpv0 watcher. Depends on the engine so all task watchers
// are registered before the pdpv0 watcher starts consuming tipsets.
func startPipeline(lc fx.Lifecycle, db *harmonydb.DB, ec ethchain.EthClient, cs *chainsched.CurioChainSched, w *pdpv0.Watcher, eng *harmonytask.TaskEngine) error {
	// Watcher callback that advances dataset state (inserts init proving-period work, etc.).
	pdpv0.NewDataSetWatch(w)
	// Curio's payment-settlement confirmation watcher (registers on the pdpv0 watcher).
	pay.NewSettleWatcher(w)
	// Watches message_waits_eth and records tx receipts / tx_success — required for
	// the sender's WaitForConfirmation (provider registration, create, addPieces).
	if _, err := message.NewMessageWatcherEth(db, eng, cs, ec); err != nil {
		return fmt.Errorf("starting eth message watcher: %w", err)
	}
	// TODO(e2e): register the remaining pdpv0 watchers for the full trigger chain.

	lc.Append(fx.Hook{OnStart: func(context.Context) error {
		// Start the pdpv0 watcher first (all AddWatcher calls are done by now), then
		// the chain scheduler that feeds tipsets into it.
		w.Run(context.Background())
		go cs.Run(context.Background())
		return nil
	}})
	return nil
}

// noopAlerting is a no-op curioalerting.AlertingInterface for Curio components that
// require one; Piri has no alerting subsystem.
type noopAlerting struct{}

func (noopAlerting) EmitEvent(context.Context, curioalerting.AlertEvent) error { return nil }
func (noopAlerting) ActivateCondition(context.Context, curioalerting.AlertCondition, string) error {
	return nil
}
func (noopAlerting) ResolveCondition(context.Context, curioalerting.AlertCondition) error {
	return nil
}
