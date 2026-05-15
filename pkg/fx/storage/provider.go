package storage

import (
	"github.com/fil-forge/ucantone/principal"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/pdp"
	"github.com/fil-forge/piri/pkg/service/blobs"
	"github.com/fil-forge/piri/pkg/service/claims"
	"github.com/fil-forge/piri/pkg/service/replicator"
	"github.com/fil-forge/piri/pkg/service/storage"
	"github.com/fil-forge/piri/pkg/service/storage/ucan"
	"github.com/fil-forge/piri/pkg/store/delegationstore"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

var Module = fx.Module("storage",
	fx.Provide(
		fx.Annotate(
			NewStorageService,
			fx.As(new(storage.Service)),
			fx.As(new(ucan.AccessDelegateService)),
			fx.As(new(ucan.BlobAllocateService)),
			fx.As(new(ucan.BlobAcceptService)),
			fx.As(new(ucan.PDPInfoService)),
			fx.As(new(ucan.ReplicaAllocateService)),
		),
	),
)

// StorageServiceParams contains all dependencies for the storage service.
//
// UploadConnection is intentionally `any` during the UCAN 1.0 migration —
// callers type-assert to `*app.ServiceConnection` to obtain the
// `{DID, *ucantone/client.HTTPClient}` pair.
type StorageServiceParams struct {
	fx.In

	Config          app.AppConfig
	ID              principal.Signer
	Blobs           blobs.Blobs
	Claims          claims.Claims
	PDP             pdp.PDP `optional:"true"`
	ReceiptStore    receiptstore.ReceiptStore
	Replicator      replicator.Replicator
	DelegationStore delegationstore.DelegationStore
}

// storageServiceWrapper wraps the storage service to implement the storage.Service interface
type storageServiceWrapper struct {
	id              principal.Signer
	blobs           blobs.Blobs
	claims          claims.Claims
	pdp             pdp.PDP
	receiptStore    receiptstore.ReceiptStore
	replicator      replicator.Replicator
	delegationStore delegationstore.DelegationStore
	uploadConn      any
}

// NewStorageService creates a new storage service
func NewStorageService(params StorageServiceParams) (storage.Service, error) {
	svc := &storageServiceWrapper{
		id:              params.ID,
		blobs:           params.Blobs,
		claims:          params.Claims,
		pdp:             params.PDP,
		receiptStore:    params.ReceiptStore,
		replicator:      params.Replicator,
		delegationStore: params.DelegationStore,
		uploadConn:      params.Config.UCANService.Services.Upload.Connection,
	}

	return svc, nil
}

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

func (s *storageServiceWrapper) Delegations() delegationstore.DelegationStore {
	return s.delegationStore
}

func (s *storageServiceWrapper) UploadConnection() any {
	return s.uploadConn
}
