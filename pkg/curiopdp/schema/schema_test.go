package schema

import (
	"net/url"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolConfig(t *testing.T) {
	t.Run("defaults when the URL carries no pool parameters", func(t *testing.T) {
		u, err := url.Parse("postgres://piri:secret@localhost:5432/piri?sslmode=disable")
		require.NoError(t, err)

		pool, err := poolConfig(*u)
		require.NoError(t, err)

		assert.Equal(t, max(4, runtime.NumCPU()), pool.MaxConnections)
		assert.Equal(t, 0, pool.MinConnections)
		assert.Equal(t, time.Hour, pool.MaxConnectionLifetime)
		assert.Equal(t, 30*time.Minute, pool.MaxIdleTime)
	})

	t.Run("reads the pool parameters from the URL", func(t *testing.T) {
		u, err := url.Parse("postgres://piri:secret@localhost:5432/piri?sslmode=disable" +
			"&pool_max_conns=40&pool_max_conn_lifetime=2h&pool_max_conn_idle_time=5m")
		require.NoError(t, err)

		pool, err := poolConfig(*u)
		require.NoError(t, err)

		assert.Equal(t, 40, pool.MaxConnections)
		assert.Equal(t, 0, pool.MinConnections)
		assert.Equal(t, 2*time.Hour, pool.MaxConnectionLifetime)
		assert.Equal(t, 5*time.Minute, pool.MaxIdleTime)
	})

	t.Run("refuses a minimum above zero", func(t *testing.T) {
		u, err := url.Parse("postgres://piri:secret@localhost:5432/piri?pool_min_conns=4")
		require.NoError(t, err)

		_, err = poolConfig(*u)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pool_min_conns")
	})

	t.Run("rejects a pool size below one", func(t *testing.T) {
		u, err := url.Parse("postgres://piri:secret@localhost:5432/piri?pool_max_conns=0")
		require.NoError(t, err)

		_, err = poolConfig(*u)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pool_max_conns")
	})

	t.Run("rejects a malformed duration", func(t *testing.T) {
		u, err := url.Parse("postgres://piri:secret@localhost:5432/piri?pool_max_conn_lifetime=soon")
		require.NoError(t, err)

		_, err = poolConfig(*u)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pool_max_conn_lifetime")
	})
}
