package pdp

import (
	"fmt"

	"github.com/fil-forge/filecoin-services/go/eip712"
	"go.uber.org/fx"
	"gorm.io/gorm"

	signerimpl "github.com/fil-forge/piri-signing-service/pkg/inprocess"
	signingservice "github.com/fil-forge/piri-signing-service/pkg/signer"
	signertypes "github.com/fil-forge/piri-signing-service/pkg/types"

	"github.com/fil-forge/piri/pkg/config/app"
	echofx "github.com/fil-forge/piri/pkg/fx/echo"
	"github.com/fil-forge/piri/pkg/pdp"
	"github.com/fil-forge/piri/pkg/pdp/aggregation/commp"
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
			fx.As(new(types.PieceWriterAPI)),
			fx.As(new(types.PieceCommPAPI)),
		),
		fx.Annotate(
			ProvideProofSetIDProvider,
		),

		fx.Annotate(
			ProvideTODOPDPImplInterface,
			fx.As(new(pdp.PDP)),
		),
		fx.Annotate(
			server.NewPDPHandler,
			fx.As(new(echofx.RouteRegistrar)),
			fx.ResultTags(`group:"route_registrar"`),
		),
	),
)

// TODO(forrest): this interface and it's impls need to be removed, renamed, or merged with the blob interface
type TODO_PDP_Impl struct {
	commpCalc commp.Calculator
	api       types.PieceAPI
}

func (s *TODO_PDP_Impl) CommpCalculate() commp.Calculator {
	return s.commpCalc
}

func (s *TODO_PDP_Impl) API() types.PieceAPI {
	return s.api
}

var _ pdp.PDP = (*TODO_PDP_Impl)(nil)

func ProvideTODOPDPImplInterface(service types.API, commpCalc commp.Calculator, cfg app.AppConfig) (*TODO_PDP_Impl, error) {
	return &TODO_PDP_Impl{
		commpCalc: commpCalc,
		api:       service,
	}, nil
}

type Params struct {
	fx.In

	ID               app.IdentityConfig
	ServerConfig     app.ServerConfig
	DB               *gorm.DB `name:"engine_db"`
	Config           app.PDPServiceConfig
	BlobStore        blobstore.PDPStore
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
		params.ID.Signer,
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
	if cfg.SigningService.Connection != nil {
		return signer.NewProofServiceSigner(cfg.SigningService.Connection.DID, cfg.SigningService.Connection.Client, proofService), nil
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
