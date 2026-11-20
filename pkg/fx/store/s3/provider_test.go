package s3

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/config/app"
	"github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/store/objectstore"
	minio_store "github.com/fil-forge/piri/pkg/store/objectstore/minio"
)

func TestNewStoresPlacesObjects(t *testing.T) {
	if os.Getenv("CI") != "" && runtime.GOOS == "darwin" {
		t.Skip("testcontainers not supported on darwin in CI")
	}
	endpoint := testutil.StartMinioContainer(t)

	testCases := []struct {
		bucketPrefix string
		bucket       string
		pdpKey       string
		ipniKey      string
	}{
		{bucketPrefix: "piri-", bucket: "piri-pdp", pdpKey: "blob", ipniKey: "ipni/advert"},
		{bucketPrefix: "fil-one/piri-0/", bucket: "fil-one", pdpKey: "piri-0/pdp/blob", ipniKey: "piri-0/ipni/advert"},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("bucket prefix %q keeps the objects in %s", tc.bucketPrefix, tc.bucket), func(t *testing.T) {
			ctx := t.Context()
			stores, err := NewStores(app.StorageConfig{S3: &app.S3Config{
				Endpoint:     endpoint,
				BucketPrefix: tc.bucketPrefix,
				Credentials:  app.Credentials{AccessKeyID: "minioadmin", SecretAccessKey: "minioadmin"},
				Insecure:     true,
			}})
			require.NoError(t, err)
			put(ctx, t, stores.PDP, "blob")
			put(ctx, t, stores.IPNI, "advert")

			bucket, err := minio_store.New(endpoint, tc.bucket, minio.Options{
				Creds: credentials.NewStaticV4("minioadmin", "minioadmin", ""),
			})
			require.NoError(t, err)
			var keys []string
			for key, err := range bucket.ListPrefix(ctx, "") {
				require.NoError(t, err)
				keys = append(keys, key)
			}
			require.ElementsMatch(t, []string{tc.pdpKey, tc.ipniKey}, keys)
		})
	}
}

func put(ctx context.Context, t *testing.T, store objectstore.Store, key string) {
	data := []byte("hello")
	require.NoError(t, store.Put(ctx, key, uint64(len(data)), bytes.NewReader(data)))
}
