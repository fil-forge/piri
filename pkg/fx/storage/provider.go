package storage

import (
	"github.com/fil-forge/go-ucanto/client"
	"github.com/fil-forge/go-ucanto/principal"
	"github.com/fil-forge/go-ucanto/validator"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/pdp"
	pdptypes "github.com/fil-forge/piri/pkg/pdp/types"
	"github.com/fil-forge/piri/pkg/service/blobs"
	"github.com/fil-forge/piri/pkg/service/claims"
	"github.com/fil-forge/piri/pkg/service/replicator"
	"github.com/fil-forge/piri/pkg/service/storage"
	blobhandler "github.com/fil-forge/piri/pkg/service/storage/handlers/blob"
	storageucan "github.com/fil-forge/piri/pkg/service/storage/ucan"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

var Module = fx.Module("storage",
	fx.Provide(
		fx.Annotate(
			NewStorageService,
			fx.As(new(storage.Service)),
		),
		NewUploadConnection,
		// Bind concrete production types to the narrow consumer interfaces
		// each handler's Deps struct declares. Each binding is a pass-through
		// that lets fx resolve a handler's narrow dep from its broader
		// concrete provider — keeps the handler-local interfaces honest about
		// what they consume without forcing every store/PDP provider to know
		// about handler-local types.
		func(a allocationstore.AllocationStore) blobhandler.AllocationStore { return a },
		func(a acceptancestore.AcceptanceStore) blobhandler.AcceptanceStore { return a },
		func(p pdptypes.PieceAPI) blobhandler.PieceAllocator { return p },
		func(p pdptypes.PieceAPI) blobhandler.PieceReader { return p },
		func(p pdptypes.PieceAPI) storageucan.PieceResolver { return p },
	),
)

// NewUploadConnection exposes the upload service connection as a top-level
// fx bean so that handlers can request it directly via their Deps structs.
func NewUploadConnection(cfg app.AppConfig) client.Connection {
	return cfg.UCANService.Services.Upload.Connection
}

// StorageServiceParams contains all dependencies for the storage service
type StorageServiceParams struct {
	fx.In

	Config                 app.AppConfig
	ID                     principal.Signer
	Blobs                  blobs.Blobs
	Claims                 claims.Claims
	PDP                    pdp.PDP
	ReceiptStore           receiptstore.ReceiptStore
	Replicator             replicator.Replicator
	ClaimValidationContext validator.ClaimContext
}

// storageServiceWrapper wraps the storage service to implement the storage.Service interface
type storageServiceWrapper struct {
	id           principal.Signer
	blobs        blobs.Blobs
	claims       claims.Claims
	pdp          pdp.PDP
	receiptStore receiptstore.ReceiptStore
	replicator   replicator.Replicator
	uploadConn   client.Connection
	claimCtx     validator.ClaimContext
}

// NewStorageService creates a new storage service
func NewStorageService(params StorageServiceParams) (storage.Service, error) {
	svc := &storageServiceWrapper{
		id:           params.ID,
		blobs:        params.Blobs,
		claims:       params.Claims,
		pdp:          params.PDP,
		receiptStore: params.ReceiptStore,
		replicator:   params.Replicator,
		uploadConn:   params.Config.UCANService.Services.Upload.Connection,
		claimCtx:     params.ClaimValidationContext,
	}

	return svc, nil
}

// Implement storage.Service interface
func (s *storageServiceWrapper) ID() principal.Signer {
	return s.id
}

func (s *storageServiceWrapper) PDP() pdp.PDP {
	return s.pdp
}

func (s *storageServiceWrapper) Blobs() blobs.Blobs {
	return s.blobs
}

func (s *storageServiceWrapper) Claims() claims.Claims {
	return s.claims
}

func (s *storageServiceWrapper) Receipts() receiptstore.ReceiptStore {
	return s.receiptStore
}

func (s *storageServiceWrapper) Replicator() replicator.Replicator {
	return s.replicator
}

func (s *storageServiceWrapper) UploadConnection() client.Connection {
	return s.uploadConn
}

func (s *storageServiceWrapper) ClaimValidationContext() validator.ClaimContext {
	return s.claimCtx
}
