package s3

import (
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/store/acceptancestore"
	"github.com/fil-forge/piri/pkg/store/allocationstore"
	"github.com/fil-forge/piri/pkg/store/blobstore"
	"github.com/fil-forge/piri/pkg/store/consolidationstore"
	"github.com/fil-forge/piri/pkg/store/delegationstore"
	minio_store "github.com/fil-forge/piri/pkg/store/objectstore/minio"
	"github.com/fil-forge/piri/pkg/store/receiptstore"
)

// Module provides bulk stores backed by S3-compatible storage. Pair with
// filesystem.LocalOnlyModule for the four stores that always remain on local
// filesystem (AggregatorDatastore, PublisherStore, RetrievalJournal,
// KeyStore).
var Module = fx.Module("s3-store",
	fx.Provide(
		NewStores,
		NewAllocationStore,
		NewAcceptanceStore,
		NewClaimStore,
		NewReceiptStore,
		NewPDPStore,
		NewConsolidationStore,
	),
)

// Stores holds all S3/MinIO store instances for different store types.
// Each store uses a separate bucket named with the configured prefix.
type Stores struct {
	Allocations   *minio_store.Store
	Acceptances   *minio_store.Store
	Claims        *minio_store.Store
	Receipts      *minio_store.Store
	PDP           *minio_store.Store
	Consolidation *minio_store.Store
}

// NewStores creates S3/MinIO stores for each bulk store, each in its own
// bucket named `{prefix}{store}`.
func NewStores(cfg app.S3Config) (*Stores, error) {
	options := minio.Options{Secure: !cfg.Insecure}
	if cfg.Credentials.AccessKeyID != "" && cfg.Credentials.SecretAccessKey != "" {
		options.Creds = credentials.NewStaticV4(
			cfg.Credentials.AccessKeyID,
			cfg.Credentials.SecretAccessKey,
			"",
		)
	}

	prefix := cfg.BucketPrefix
	endpoint := cfg.Endpoint
	stores := &Stores{}
	var err error

	if stores.Allocations, err = minio_store.New(endpoint, prefix+"allocations", options); err != nil {
		return nil, fmt.Errorf("creating allocations s3 store: %w", err)
	}
	if stores.Acceptances, err = minio_store.New(endpoint, prefix+"acceptances", options); err != nil {
		return nil, fmt.Errorf("creating acceptances s3 store: %w", err)
	}
	if stores.Claims, err = minio_store.New(endpoint, prefix+"claims", options); err != nil {
		return nil, fmt.Errorf("creating claims s3 store: %w", err)
	}
	if stores.Receipts, err = minio_store.New(endpoint, prefix+"receipts", options); err != nil {
		return nil, fmt.Errorf("creating receipts s3 store: %w", err)
	}
	if stores.PDP, err = minio_store.New(endpoint, prefix+"pdp", options); err != nil {
		return nil, fmt.Errorf("creating pdp s3 store: %w", err)
	}
	if stores.Consolidation, err = minio_store.New(endpoint, prefix+"consolidation", options); err != nil {
		return nil, fmt.Errorf("creating consolidation s3 store: %w", err)
	}

	return stores, nil
}

func NewAllocationStore(stores *Stores) allocationstore.AllocationStore {
	return allocationstore.NewS3Store(stores.Allocations)
}

func NewAcceptanceStore(stores *Stores) acceptancestore.AcceptanceStore {
	return acceptancestore.NewS3Store(stores.Acceptances)
}

func NewClaimStore(stores *Stores) delegationstore.DelegationStore {
	return delegationstore.NewS3Store(stores.Claims)
}

func NewReceiptStore(stores *Stores) receiptstore.ReceiptStore {
	return receiptstore.NewS3Store(stores.Receipts)
}

// NewPDPStore provides the blob store backing PDP piece storage.
func NewPDPStore(stores *Stores) blobstore.Blobstore {
	return blobstore.NewS3Store(stores.PDP)
}

func NewConsolidationStore(stores *Stores) consolidationstore.Store {
	return consolidationstore.NewS3Store(stores.Consolidation)
}
