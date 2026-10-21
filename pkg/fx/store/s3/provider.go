package s3

import (
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/store/blobstore"
	minio_store "github.com/fil-forge/piri/pkg/store/objectstore/minio"
)

// Module provides the blob store backed by S3-compatible storage.
// Use this module alongside filesystem.LocalOnlyModule which provides
// stores that must remain on the local filesystem (AggregatorDatastore,
// RetrievalJournal, KeyStore), and postgres.Module, which
// provides the record stores.
var Module = fx.Module("s3-store",
	fx.Provide(
		ProvideConfigs,
		NewStores,
		NewPDPStore,
	),
)

// Stores holds the S3/MinIO store instances, each in its own bucket named
// with the configured prefix.
type Stores struct {
	// PDP holds blob bytes, in the bucket {prefix}pdp.
	PDP *minio_store.Store
}

// NewStores creates the S3/MinIO stores.
func NewStores(cfg app.StorageConfig) (*Stores, error) {
	if cfg.S3 == nil || cfg.S3.Endpoint == "" || cfg.S3.BucketPrefix == "" {
		return nil, fmt.Errorf("S3 configuration required: endpoint and bucket_prefix must be set")
	}

	options := minio.Options{Secure: !cfg.S3.Insecure}
	if cfg.S3.Credentials.AccessKeyID != "" && cfg.S3.Credentials.SecretAccessKey != "" {
		options.Creds = credentials.NewStaticV4(
			cfg.S3.Credentials.AccessKeyID,
			cfg.S3.Credentials.SecretAccessKey,
			"",
		)
	}

	prefix := cfg.S3.BucketPrefix
	endpoint := cfg.S3.Endpoint
	stores := &Stores{}
	var err error

	if stores.PDP, err = minio_store.New(endpoint, prefix+"pdp", options); err != nil {
		return nil, fmt.Errorf("creating pdp s3 store: %w", err)
	}

	return stores, nil
}

// Configs provides storage configs needed by S3-backed stores.
type Configs struct {
	fx.Out
	Blob  app.BlobStorageConfig
	Stash app.StashStoreConfig
	PDP   app.PDPStoreConfig
}

// ProvideConfigs extracts configs for S3-backed stores.
func ProvideConfigs(cfg app.StorageConfig) Configs {
	return Configs{
		Blob:  cfg.Blobs,
		Stash: cfg.StashStore,
		PDP:   cfg.PDPStore,
	}
}

// NewPDPStore provides the blob store backing PDP piece storage.
func NewPDPStore(stores *Stores) *blobstore.Store {
	return blobstore.NewS3Store(stores.PDP)
}
