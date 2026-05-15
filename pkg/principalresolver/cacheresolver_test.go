package principalresolver_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fil-forge/ucantone/did"
	"github.com/stretchr/testify/require"

	"github.com/fil-forge/piri/pkg/principalresolver"
)

type mockResolver struct {
	resolveFn func(context.Context, did.DID) ([]did.DID, error)
	callCount int32
}

func (m *mockResolver) Resolve(ctx context.Context, input did.DID) ([]did.DID, error) {
	atomic.AddInt32(&m.callCount, 1)
	if m.resolveFn != nil {
		return m.resolveFn(ctx, input)
	}
	return nil, errors.New("mock error")
}

func (m *mockResolver) getCallCount() int {
	return int(atomic.LoadInt32(&m.callCount))
}

func TestNewCachedResolver(t *testing.T) {
	t.Run("creates resolver with valid TTL", func(t *testing.T) {
		resolver, err := principalresolver.NewCachedResolver(&mockResolver{}, 5*time.Minute)
		require.NoError(t, err)
		require.NotNil(t, resolver)
	})

	t.Run("creates resolver with zero TTL", func(t *testing.T) {
		resolver, err := principalresolver.NewCachedResolver(&mockResolver{}, 0)
		require.NoError(t, err)
		require.NotNil(t, resolver)
	})
}

func TestCachedResolver_Resolve(t *testing.T) {
	t.Run("caches successful resolution", func(t *testing.T) {
		didWeb, err := did.Parse("did:web:example.com")
		require.NoError(t, err)
		didKey, err := did.Parse("did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK")
		require.NoError(t, err)

		mock := &mockResolver{
			resolveFn: func(_ context.Context, input did.DID) ([]did.DID, error) {
				if input.String() == didWeb.String() {
					return []did.DID{didKey}, nil
				}
				return nil, fmt.Errorf("not found")
			},
		}

		resolver, err := principalresolver.NewCachedResolver(mock, 100*time.Millisecond)
		require.NoError(t, err)

		// First call hits the wrapped resolver
		got, err := resolver.Resolve(t.Context(), didWeb)
		require.NoError(t, err)
		require.Equal(t, []did.DID{didKey}, got)
		require.Equal(t, 1, mock.getCallCount())

		// Second call uses cache
		got, err = resolver.Resolve(t.Context(), didWeb)
		require.NoError(t, err)
		require.Equal(t, []did.DID{didKey}, got)
		require.Equal(t, 1, mock.getCallCount())

		// After TTL expires the wrapped resolver is hit again
		time.Sleep(150 * time.Millisecond)
		got, err = resolver.Resolve(t.Context(), didWeb)
		require.NoError(t, err)
		require.Equal(t, []did.DID{didKey}, got)
		require.Equal(t, 2, mock.getCallCount())
	})

	t.Run("does not cache errors", func(t *testing.T) {
		didWeb, err := did.Parse("did:web:example.com")
		require.NoError(t, err)

		mock := &mockResolver{
			resolveFn: func(_ context.Context, _ did.DID) ([]did.DID, error) {
				return nil, fmt.Errorf("resolution failed")
			},
		}

		resolver, err := principalresolver.NewCachedResolver(mock, 100*time.Millisecond)
		require.NoError(t, err)

		_, err = resolver.Resolve(t.Context(), didWeb)
		require.Error(t, err)
		require.Equal(t, 1, mock.getCallCount())

		_, err = resolver.Resolve(t.Context(), didWeb)
		require.Error(t, err)
		require.Equal(t, 2, mock.getCallCount())
	})

	t.Run("handles concurrent access", func(t *testing.T) {
		didWeb, err := did.Parse("did:web:example.com")
		require.NoError(t, err)
		didKey, err := did.Parse("did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK")
		require.NoError(t, err)

		var resolverCalls int32
		mock := &mockResolver{
			resolveFn: func(_ context.Context, _ did.DID) ([]did.DID, error) {
				atomic.AddInt32(&resolverCalls, 1)
				time.Sleep(10 * time.Millisecond)
				return []did.DID{didKey}, nil
			},
		}

		resolver, err := principalresolver.NewCachedResolver(mock, time.Second)
		require.NoError(t, err)

		var wg sync.WaitGroup
		results := make([][]did.DID, 10)
		errs := make([]error, 10)

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				results[idx], errs[idx] = resolver.Resolve(t.Context(), didWeb)
			}(i)
		}
		wg.Wait()

		for i := 0; i < 10; i++ {
			require.NoError(t, errs[i])
			require.Equal(t, []did.DID{didKey}, results[i])
		}

		actualCalls := atomic.LoadInt32(&resolverCalls)
		require.LessOrEqual(t, actualCalls, int32(10))
		_, err = resolver.Resolve(t.Context(), didWeb)
		require.NoError(t, err)
		require.Equal(t, actualCalls, atomic.LoadInt32(&resolverCalls))
	})

	t.Run("handles different DIDs independently", func(t *testing.T) {
		did1, err := did.Parse("did:web:example1.com")
		require.NoError(t, err)
		did2, err := did.Parse("did:web:example2.com")
		require.NoError(t, err)
		didKey1, err := did.Parse("did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK")
		require.NoError(t, err)
		didKey2, err := did.Parse("did:key:z6Mkfriq1MqLBoPWecGoDLjguo1sB9brj6wT3qZ5BxkKpuP6")
		require.NoError(t, err)

		mock := &mockResolver{
			resolveFn: func(_ context.Context, input did.DID) ([]did.DID, error) {
				switch input.String() {
				case did1.String():
					return []did.DID{didKey1}, nil
				case did2.String():
					return []did.DID{didKey2}, nil
				default:
					return nil, fmt.Errorf("unknown DID")
				}
			},
		}

		resolver, err := principalresolver.NewCachedResolver(mock, time.Second)
		require.NoError(t, err)

		got, err := resolver.Resolve(t.Context(), did1)
		require.NoError(t, err)
		require.Equal(t, []did.DID{didKey1}, got)
		require.Equal(t, 1, mock.getCallCount())

		got, err = resolver.Resolve(t.Context(), did2)
		require.NoError(t, err)
		require.Equal(t, []did.DID{didKey2}, got)
		require.Equal(t, 2, mock.getCallCount())

		// Repeats hit the cache.
		_, _ = resolver.Resolve(t.Context(), did1)
		_, _ = resolver.Resolve(t.Context(), did2)
		require.Equal(t, 2, mock.getCallCount())
	})
}

func TestCachedResolver_WithMapResolver(t *testing.T) {
	mapping := map[string]string{
		"did:web:alice.example.com": "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
		"did:web:bob.example.com":   "did:key:z6Mkfriq1MqLBoPWecGoDLjguo1sB9brj6wT3qZ5BxkKpuP6",
	}

	mapResolver, err := principalresolver.NewMapResolver(mapping)
	require.NoError(t, err)

	cachedResolver, err := principalresolver.NewCachedResolver(mapResolver, time.Second)
	require.NoError(t, err)

	aliceDID, err := did.Parse("did:web:alice.example.com")
	require.NoError(t, err)
	aliceKey, err := did.Parse("did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK")
	require.NoError(t, err)

	got, err := cachedResolver.Resolve(t.Context(), aliceDID)
	require.NoError(t, err)
	require.Equal(t, []did.DID{aliceKey}, got)

	unknownDID, err := did.Parse("did:web:unknown.example.com")
	require.NoError(t, err)
	_, err = cachedResolver.Resolve(t.Context(), unknownDID)
	require.Error(t, err)
}

func TestNewMapResolver_Invalid(t *testing.T) {
	_, err := principalresolver.NewMapResolver(map[string]string{
		"invalid-did": "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
	})
	require.Error(t, err)
}
