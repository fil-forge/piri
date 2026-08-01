package pdp

import (
	"fmt"

	"github.com/fil-forge/filecoin-services/go/eip712"
	"go.uber.org/fx"

	signerimpl "github.com/fil-forge/piri-signing-service/pkg/inprocess"
	signingservice "github.com/fil-forge/piri-signing-service/pkg/signer"
	signertypes "github.com/fil-forge/piri-signing-service/pkg/types"

	// curio infra
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/tasks/message"

	"github.com/fil-forge/piri/pkg/config/app"
	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/pdp/httpapi/server"
	"github.com/fil-forge/piri/pkg/pdp/service"
	"github.com/fil-forge/piri/pkg/pdp/smartcontracts"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/service/proofs"
	"github.com/fil-forge/piri/pkg/service/signer"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/blobstore"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

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
)

type Params struct {
	fx.In

	ID               app.IdentityConfig
	ServerConfig     app.ServerConfig
	DB               *harmonydb.DB // curio harmonydb (unnamed; provided by curiopdp.Module) — single DB surface
	Config           app.PDPServiceConfig
	BlobStore        blobstore.Blobstore
	AcceptanceStore  acceptancestore.AcceptanceStore
	AllocationStore  allocationstore.AllocationStore
	ReceiptStore     receiptstore.ReceiptStore
	Resolver         types.PieceResolverAPI
	Reader           types.PieceReaderAPI
	Sender           *message.SenderETH
	ChainScheduler   *chainsched.CurioChainSched
	ChainClient      service.ChainClient
	EthClient        ethchain.EthClient // raw eth client — contract.FSRegister signs/sends the register tx and reads balance
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
		params.AllocationStore,
		params.ReceiptStore,
		params.Resolver,
		params.Reader,
		params.Sender,
		params.ChainScheduler,
		params.ChainClient,
		params.EthClient,
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
