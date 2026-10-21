package minio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/internal/testutil"
	"github.com/fil-forge/piri/pkg/store/objectstore"
)

func TestBucketCreation(t *testing.T) {
	t.Run("create new bucket", func(t *testing.T) {
		bucketName := uniqueBucketName(t.Name())
		store := createTestStore(t, bucketName)
		require.NotNil(t, store)
	})

	t.Run("use existing bucket", func(t *testing.T) {
		bucketName := uniqueBucketName(t.Name())

		// Create first store (creates bucket)
		store1 := createTestStore(t, bucketName)
		require.NotNil(t, store1)

		// Create second store (uses existing bucket)
		store2, err := New(minioEndpoint, bucketName, minio.Options{
			Creds:  credentials.NewStaticV4("minioadmin", "minioadmin", ""),
			Secure: false,
		})
		require.NoError(t, err)
		require.NotNil(t, store2)
	})
}

var (
	minioEndpoint string
)

func TestMain(m *testing.M) {
	if os.Getenv("CI") != "" && runtime.GOOS == "darwin" {
		fmt.Println("Skipping darwin tests, testcontainers not supported in CI")
		os.Exit(0)
	}
	logging.SetDebugLogging()
	ctx := context.Background()

	container, err := testutil.RunMinioContainer(ctx)
	if err != nil {
		panic(fmt.Sprintf("Failed to start MinIO container: %v", err))
	}

	minioEndpoint, err = container.ConnectionString(ctx)
	if err != nil {
		panic(fmt.Sprintf("Failed to get container endpoint: %v", err))
	}

	code := m.Run()

	if err := container.Terminate(ctx); err != nil {
		panic(fmt.Sprintf("Failed to terminate container: %v", err))
	}

	os.Exit(code)
}

func createTestStore(t *testing.T, bucketName string) *Store {
	store, err := New(minioEndpoint, bucketName, minio.Options{
		Creds:  credentials.NewStaticV4("minioadmin", "minioadmin", ""),
		Secure: false,
	})
	require.NoError(t, err)
	require.NotNil(t, store)
	require.True(t, store.client.IsOnline())
	return store
}

func uniqueBucketName(testName string) string {
	// S3 bucket naming rules:
	// - Must be 3-63 characters
	// - Can only contain lowercase letters, numbers, and hyphens
	// - Cannot start or end with hyphen
	// - Cannot contain underscores or consecutive hyphens
	sanitized := strings.ToLower(testName)
	sanitized = strings.ReplaceAll(sanitized, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, "_", "-")
	sanitized = strings.ReplaceAll(sanitized, " ", "-")

	// Remove any non-alphanumeric characters except hyphens
	var result []rune
	for _, r := range sanitized {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result = append(result, r)
		}
	}
	sanitized = string(result)

	// Ensure no consecutive hyphens
	for strings.Contains(sanitized, "--") {
		sanitized = strings.ReplaceAll(sanitized, "--", "-")
	}

	// Trim hyphens from start and end
	sanitized = strings.Trim(sanitized, "-")

	// Create bucket name with timestamp
	ts := fmt.Sprintf("%d", time.Now().UnixNano())
	bucketName := fmt.Sprintf("test-%s-%s", sanitized, ts[len(ts)-8:])

	// Ensure max 63 chars
	if len(bucketName) > 63 {
		// Keep last 8 chars of timestamp and adjust test name
		maxTestNameLen := 63 - 6 - 8 // "test-" (5) + "-" (1) + timestamp (8)
		if len(sanitized) > maxTestNameLen {
			sanitized = sanitized[:maxTestNameLen]
		}
		bucketName = fmt.Sprintf("test-%s-%s", sanitized, ts[len(ts)-8:])
	}

	return bucketName
}

func TestMove(t *testing.T) {
	ctx := t.Context()
	store := createTestStore(t, uniqueBucketName(t.Name()))
	get := func(key string) ([]byte, error) {
		obj, err := store.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		defer obj.Body().Close()
		return io.ReadAll(obj.Body())
	}

	data := []byte("staged bytes")
	require.NoError(t, store.Put(ctx, "staged-one", uint64(len(data)), bytes.NewReader(data)))
	require.NoError(t, store.Move(ctx, "staged-one", "blob/one.data"))
	got, err := get("blob/one.data")
	require.NoError(t, err)
	require.Equal(t, data, got)
	_, err = get("staged-one")
	require.ErrorIs(t, err, objectstore.ErrNotExist, "the source is removed")

	t.Run("a move already made succeeds", func(t *testing.T) {
		require.NoError(t, store.Move(ctx, "staged-one", "blob/one.data"))
	})

	t.Run("a move interrupted after the copy is finished", func(t *testing.T) {
		require.NoError(t, store.Put(ctx, "staged-two", uint64(len(data)), bytes.NewReader(data)))
		require.NoError(t, store.Put(ctx, "blob/two.data", uint64(len(data)), bytes.NewReader(data)))
		require.NoError(t, store.Move(ctx, "staged-two", "blob/two.data"))
		_, err := get("staged-two")
		require.ErrorIs(t, err, objectstore.ErrNotExist)
	})

	t.Run("moving nothing fails", func(t *testing.T) {
		require.ErrorIs(t, store.Move(ctx, "staged-none", "blob/none.data"), objectstore.ErrNotExist)
	})
}
