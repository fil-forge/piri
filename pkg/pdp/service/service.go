package service

import (
	"context"
	"net/url"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/fil-forge/filecoin-services/go/eip712"
	signer "github.com/fil-forge/piri-signing-service/pkg/types"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	filtypes "github.com/filecoin-project/lotus/chain/types"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/sync/singleflight"

	// curio infra
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/tasks/message"

	appconfig "github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/pdp/smartcontracts"
	"github.com/fil-forge/piri/pkg/pdp/tasks"
	"github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/blobstore"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

var log = logging.Logger("pdp/service")

var _ types.API = (*PDPService)(nil)

type ChainClient interface {
	ChainHead(ctx context.Context) (*filtypes.TipSet, error)
	ChainNotify(ctx context.Context) (<-chan []*api.HeadChange, error)
	StateGetRandomnessDigestFromBeacon(ctx context.Context, randEpoch abi.ChainEpoch, tsk filtypes.TipSetKey) (abi.Randomness, error)
}

type EthClient interface {
	tasks.SenderETHClient
	tasks.MessageWatcherEthClient
	bind.ContractBackend
}

type PDPService struct {
	cfg             appconfig.PDPServiceConfig
	id              ucan.Issuer
	endpoint        url.URL
	address         common.Address
	blobstore       blobstore.Blobstore
	acceptanceStore acceptancestore.AcceptanceStore
	receiptStore    receiptstore.ReceiptStore
	chainClient     ChainClient

	// ethClient lets Curio's contract.FSRegister sign/send the registerProvider
	// tx and read the wallet balance (via BalanceAt) — no Lotus node required.
	ethClient ethchain.EthClient

	// db is the single DB surface — Curio harmonydb (Postgres), backing every
	// PDP table (pipeline + piece/commP mapping + parked pieces).
	db *harmonydb.DB

	name string

	pieceResolver types.PieceResolverAPI
	pieceReader   types.PieceReaderAPI

	// curio infra (replaces piri sender/engine/chainScheduler)
	sender         *message.SenderETH
	engine         *harmonytask.TaskEngine
	chainScheduler *chainsched.CurioChainSched

	signingService signer.SigningService

	commPGroup singleflight.Group

	edc              *eip712.ExtraDataEncoder
	verifierContract smartcontracts.Verifier
	serviceContract  smartcontracts.Service
	registryContract smartcontracts.Registry

	maxPieceSizeLog2Cache bigIntCache
}

func New(
	cfg appconfig.PDPServiceConfig,
	id ucan.Issuer,
	endpoint url.URL,
	db *harmonydb.DB, // curio harmonydb — single DB surface
	bs blobstore.Blobstore,
	acceptanceStore acceptancestore.AcceptanceStore,
	receiptStore receiptstore.ReceiptStore,
	resolver types.PieceResolverAPI,
	reader types.PieceReaderAPI,
	sender *message.SenderETH,
	engine *harmonytask.TaskEngine,
	chainScheduler *chainsched.CurioChainSched,
	chainClient ChainClient,
	ethClient ethchain.EthClient,
	signingService signer.SigningService,
	edc *eip712.ExtraDataEncoder,
	verifier smartcontracts.Verifier,
	serviceContract smartcontracts.Service,
	registryContract smartcontracts.Registry,
) (*PDPService, error) {
	return &PDPService{
		cfg:              cfg,
		id:               id,
		endpoint:         endpoint,
		address:          cfg.OwnerAddress,
		db:               db,
		name:             "storacha",
		pieceResolver:    resolver,
		pieceReader:      reader,
		blobstore:        bs,
		acceptanceStore:  acceptanceStore,
		receiptStore:     receiptStore,
		sender:           sender,
		engine:           engine,
		chainScheduler:   chainScheduler,
		chainClient:      chainClient,
		ethClient:        ethClient,
		signingService:   signingService,
		edc:              edc,
		verifierContract: verifier,
		serviceContract:  serviceContract,
		registryContract: registryContract,
	}, nil
}
