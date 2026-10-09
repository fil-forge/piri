package keylock

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocks(t *testing.T) {
	var l Locks
	var wg sync.WaitGroup
	var mu sync.Mutex
	held := map[string]int{}
	for i := range 100 {
		key := []string{"a", "b"}[i%2]
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer l.Lock(key)()
			mu.Lock()
			held[key]++
			assert.Equal(t, 1, held[key], "one holder per key")
			mu.Unlock()
			mu.Lock()
			held[key]--
			mu.Unlock()
		}()
	}
	wg.Wait()
	require.Empty(t, l.locks, "unused locks are dropped")
}
