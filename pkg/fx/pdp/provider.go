package pdp

import (
	"context"
	"fmt"
	"time"

	"github.com/fil-forge/filecoin-services/go/eip712"
	logging "github.com/ipfs/go-log/v2"
	"go.uber.org/fx"
	"gorm.io/gorm"

	signerimpl "github.com/fil-forge/piri-signing-service/pkg/inprocess"
	signingservice "github.com/fil-forge/piri-signing-service/pkg/signer"
	signertypes "github.com/fil-forge/piri-signing-service/pkg/types"

	"github.com/fil-forge/piri/pkg/config/app"
	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/pdp/chainsched"
	"github.com/fil-forge/piri/pkg/pdp/ethereum"
	"github.com/fil-forge/piri/pkg/pdp/httpapi/server"
	"github.com/fil-forge/piri/pkg/pdp/scheduler"
	"github.com/fil-forge/piri/pkg/pdp/service"
	"github.com/fil-forge/piri/pkg/pdp/smartcontracts"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/service/proofs"
	"github.com/fil-forge/piri/pkg/service/signer"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/blobstore"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

var log = logging.Logger("fx/pdp")

var Module = fx.Module("pdp-service",
	fx.Provide(
		eip712.NewExtraDataEncoder,
		ProvideSigningService,
		fx.Annotate(
			ProvidePDPService,
			fx.As(fx.Self()),      // provide service as concrete type
			fx.As(new(types.API)), // also provide the server as the interface(s) it implements
			fx.As(new(types.ProofSetAPI)),
			fx.As(new(types.PieceAPI)),
			// PieceReaderAPI is intentionally NOT exposed via PDPService:
			// PDPService.Params.Reader already consumes PieceReaderAPI from
			// NewStoreReader. Listing it here too creates a self-dependency
			// cycle (PDPService provides AND consumes PieceReaderAPI) and
			// double-registers the type. Consumers receive StoreReader
			// directly; PDPService's Read/Has methods are still callable on
			// concrete *PDPService receivers.
			fx.As(new(types.PieceWriterAPI)),
			fx.As(new(types.PieceCommPAPI)),
			fx.As(new(types.PieceRemoverAPI)),
		),
		ProvideProofSetIDProvider,
		fx.Annotate(
			server.NewPDPHandler,
			fx.As(new(echofx.RouteRegistrar)),
			fx.ResultTags(`group:"route_registrar"`),
		),
	),
	fx.Invoke(StartRemovalSweeper),
)

// RemovalSweepInterval is how often queued blob removals are advanced
// (root retirement + post-confirmation byte deletion).
const RemovalSweepInterval = 30 * time.Second

// StartRemovalSweeper runs ProcessPendingRemovals on a fixed interval for
// the lifetime of the application.
func StartRemovalSweeper(lc fx.Lifecycle, remover types.PieceRemoverAPI) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				defer close(done)
				ticker := time.NewTicker(RemovalSweepInterval)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if err := remover.ProcessPendingRemovals(ctx); err != nil {
							log.Errorw("processing pending removals", "err", err)
						}
					}
				}
			}()
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			<-done
			return nil
		},
	})
}

type Params struct {
	fx.In

	ID               app.IdentityConfig
	ServerConfig     app.ServerConfig
	DB               *gorm.DB `name:"engine_db"`
	Config           app.PDPServiceConfig
	BlobStore        blobstore.Blobstore
	AcceptanceStore  acceptancestore.AcceptanceStore
	ReceiptStore     receiptstore.ReceiptStore
	Resolver         types.PieceResolverAPI
	Reader           types.PieceReaderAPI
	Sender           ethereum.Sender
	Engine           *scheduler.TaskEngine
	ChainScheduler   *chainsched.Scheduler
	ChainClient      service.ChainClient
	SigningService   signertypes.SigningService
	ExtraDataEncoder *eip712.ExtraDataEncoder
	Verifier         smartcontracts.Verifier
	Service          smartcontracts.Service
	Registry         smartcontracts.Registry
}

func ProvidePDPService(params Params) (*service.PDPService, error) {
	return service.New(
		params.Config,
		params.ID.Issuer,
		params.ServerConfig.PublicURL,
		params.DB,
		params.BlobStore,
		params.AcceptanceStore,
		params.ReceiptStore,
		params.Resolver,
		params.Reader,
		params.Sender,
		params.Engine,
		params.ChainScheduler,
		params.ChainClient,
		params.SigningService,
		params.ExtraDataEncoder,
		params.Verifier,
		params.Service,
		params.Registry,
	)
}

func ProvideProofSetIDProvider(cfg app.UCANServiceConfig) (types.ProofSetIDProvider, error) {
	return &service.ConfiguredProofSetProvider{ID: cfg.ProofSetID}, nil
}

func ProvideSigningService(cfg app.PDPServiceConfig, proofService proofs.ProofService) (signertypes.SigningService, error) {
	if cfg.SigningService.Client != nil {
		sc := cfg.SigningService.Client
		return signer.NewProofServiceSigner(sc, sc.ServiceDID, sc.HTTP, proofService), nil
	} else if cfg.SigningService.PrivateKey != nil {
		s := signingservice.NewSigner(
			cfg.SigningService.PrivateKey,
			cfg.ChainID,
			cfg.Contracts.Service,
		)
		return signerimpl.New(s), nil
	}

	return nil, fmt.Errorf("no signer configured")
}
