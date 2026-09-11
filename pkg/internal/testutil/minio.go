package testutil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"
	"github.com/testcontainers/testcontainers-go/wait"
)

// RunMinioContainer starts a MinIO container and waits until its object layer
// is ready to serve requests.
//
// The testcontainers minio module's default wait strategy polls
// /minio/health/live, which returns 200 as soon as the process accepts
// connections — before the object layer is initialized — so a request issued
// immediately after startup can fail with a transient "Server not
// initialized" error. /minio/health/ready behaves the same way (it only sets
// the x-minio-server-status header). /minio/health/cluster is the endpoint
// that returns 503 until the object layer is up, so wait on that instead.
func RunMinioContainer(ctx context.Context) (*minio.MinioContainer, error) {
	// Our own build of MinIO. Upstream withdrew minio/minio from Docker Hub on
	// 2026-09-11 -- every tag, pinned releases included -- and archived the
	// project, so there is no public image left to pull. fil-forge/minio builds
	// it from source at the tag below.
	return minio.Run(ctx, "ghcr.io/fil-forge/minio:RELEASE.2025-10-15T17-29-55Z",
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/minio/health/cluster").WithPort("9000/tcp"),
		),
	)
}

func StartMinioContainer(t *testing.T) string {
	container, err := RunMinioContainer(t.Context())
	testcontainers.CleanupContainer(t, container)
	require.NoError(t, err)

	endpoint, err := container.ConnectionString(t.Context())
	require.NoError(t, err)

	t.Logf("Minio listening on: http://%s", endpoint)
	return endpoint
}
