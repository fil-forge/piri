package minio

import (
	"context"
	"errors"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/require"
)

func TestIsServerNotInitialized(t *testing.T) {
	cases := map[string]struct {
		err      error
		expected bool
	}{
		"nil error": {
			err:      nil,
			expected: false,
		},
		"minio server-not-initialized code": {
			err: minio.ErrorResponse{
				Code:    "XMinioServerNotInitialized",
				Message: "Server not initialized yet, please try again.",
			},
			expected: true,
		},
		"plain error mentioning server not initialized": {
			err:      errors.New("failed to check if bucket foo exists: Server not initialized yet, please try again."),
			expected: true,
		},
		"unrelated minio error": {
			err: minio.ErrorResponse{
				Code:    "NoSuchBucket",
				Message: "The specified bucket does not exist",
			},
			expected: false,
		},
		"context deadline exceeded": {
			err:      context.DeadlineExceeded,
			expected: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.expected, isServerNotInitialized(tc.err))
		})
	}
}
