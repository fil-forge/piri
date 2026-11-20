package s3

import (
	"fmt"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/fx"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/store/blobstore"
	"github.com/fil-forge/piri/pkg/store/objectstore"
	minio_store "github.com/fil-forge/piri/pkg/store/objectstore/minio"
)

// Module provides the blob store backed by S3-compatible storage, and the
// IPNI adverts kept beside the blobs.
// Use this module alongside filesystem.LocalOnlyModule which provides
// stores that must remain on the local filesystem (RetrievalJournal,
// KeyStore), and postgres.Module, which provides the record stores.
var Module = fx.Module("s3-store",
	fx.Provide(
		ProvideConfigs,
		NewStores,
		NewPDPStore,
		fx.Annotate(
			NewIPNIObjectStore,
			fx.ResultTags(`name:"ipni_objectstore"`),
		),
	),
)

// Stores holds the S3/MinIO stores.
type Stores struct {
	// PDP holds blob bytes.
	PDP *minio_store.Store
	// IPNI holds the node's IPNI adverts, one object per advert.
	IPNI objectstore.Store
}

// NewStores creates the S3/MinIO stores where the config places them.
//
// A bucket prefix names a bucket of Piri's own for the blobs, {prefix}pdp,
// and the IPNI adverts go under ipni/ in that bucket.
//
// A path places both in one bucket, shared with other data: the part before
// the first "/" names the bucket and the rest is a folder in it. For
// "fil-one/piri-0/", the blobs go under piri-0/pdp/ and the IPNI adverts
// under piri-0/ipni/ in the bucket fil-one.
func NewStores(cfg app.StorageConfig) (*Stores, error) {
	if cfg.S3 == nil || cfg.S3.Endpoint == "" || (cfg.S3.BucketPrefix == "" && cfg.S3.Path == "") {
		return nil, fmt.Errorf("S3 configuration required: endpoint and bucket_prefix or path must be set")
	}

	options := minio.Options{Secure: !cfg.S3.Insecure}
	if cfg.S3.Credentials.AccessKeyID != "" && cfg.S3.Credentials.SecretAccessKey != "" {
		options.Creds = credentials.NewStaticV4(
			cfg.S3.Credentials.AccessKeyID,
			cfg.S3.Credentials.SecretAccessKey,
			"",
		)
	}

	endpoint := cfg.S3.Endpoint
	if cfg.S3.Path == "" {
		pdp, err := minio_store.New(endpoint, cfg.S3.BucketPrefix+"pdp", options)
		if err != nil {
			return nil, fmt.Errorf("creating pdp s3 store: %w", err)
		}
		return &Stores{PDP: pdp, IPNI: objectstore.Prefixed("ipni/", pdp)}, nil
	}

	bucket, folder, _ := strings.Cut(cfg.S3.Path, "/")
	pdp, err := minio_store.New(endpoint, bucket, options, minio_store.WithKeyPrefix(folder+"pdp/"))
	if err != nil {
		return nil, fmt.Errorf("creating pdp s3 store: %w", err)
	}
	ipni, err := minio_store.New(endpoint, bucket, options, minio_store.WithKeyPrefix(folder+"ipni/"))
	if err != nil {
		return nil, fmt.Errorf("creating ipni s3 store: %w", err)
	}
	return &Stores{PDP: pdp, IPNI: ipni}, nil
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

// NewIPNIObjectStore provides the node's IPNI adverts, one object per advert
// keyed by its CID. It uses the raw object store rather than the blob store,
// whose key encoder would rewrite the CID keys.
func NewIPNIObjectStore(stores *Stores) objectstore.Store {
	return objectstore.Traced("ipni", stores.IPNI)
}

// NewPDPStore provides the blob store backing PDP piece storage.
func NewPDPStore(stores *Stores) *blobstore.Store {
	return blobstore.NewS3Store(stores.PDP)
}
