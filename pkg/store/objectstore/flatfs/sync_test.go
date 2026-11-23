package flatfs_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/fil-forge/piri/pkg/store/objectstore"
	flatfs "github.com/fil-forge/piri/pkg/store/objectstore/flatfs"
)

// Piri's filesystem nodes open their IPNI and PDP stores with sync on, so
// these tests cover the fsync paths that the other tests, all with sync off,
// leave out.

func TestSyncingStorePutsAnObject(t *testing.T) {
	fs := newSyncingStore(t, t.TempDir())
	put(t, fs, "abcd", "value")
	require.Equal(t, "value", get(t, fs, "abcd"))
}

func TestSyncingStoreOverwritesAnObject(t *testing.T) {
	fs := newSyncingStore(t, t.TempDir())
	put(t, fs, "abcd", "old")
	put(t, fs, "abcd", "new")
	require.Equal(t, "new", get(t, fs, "abcd"))
}

func TestSyncingStorePutLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	fs := newSyncingStore(t, dir)
	put(t, fs, "abcd", "value")
	entries, err := os.ReadDir(filepath.Join(dir, ".temp"))
	require.NoError(t, err)
	require.Empty(t, entries)
}

// The keys land in different shard directories, so the move syncs both.
func TestSyncingStoreMovesAnObject(t *testing.T) {
	fs := newSyncingStore(t, t.TempDir())
	put(t, fs, "staged-abcd", "value")
	require.NoError(t, fs.Move(t.Context(), "staged-abcd", "wxyz"))
	require.Equal(t, "value", get(t, fs, "wxyz"))
}

func TestSyncingStoreMoveRemovesTheSource(t *testing.T) {
	fs := newSyncingStore(t, t.TempDir())
	put(t, fs, "staged-abcd", "value")
	require.NoError(t, fs.Move(t.Context(), "staged-abcd", "wxyz"))
	_, err := fs.Get(t.Context(), "staged-abcd")
	require.ErrorIs(t, err, objectstore.ErrNotExist)
}

// A retry of a move whose rename went through but whose sync failed
// succeeds, after syncing the directories.
func TestSyncingStoreRetriedMoveSucceeds(t *testing.T) {
	fs := newSyncingStore(t, t.TempDir())
	put(t, fs, "staged-abcd", "value")
	require.NoError(t, fs.Move(t.Context(), "staged-abcd", "wxyz"))
	require.NoError(t, fs.Move(t.Context(), "staged-abcd", "wxyz"))
}

// The keys share a shard directory that none of the writes finds present,
// so all but one wait for the one creating it.
func TestSyncingStoreConcurrentPutsIntoANewShard(t *testing.T) {
	fs := newSyncingStore(t, t.TempDir())
	want := make(map[string]string)
	for i := range 32 {
		want[fmt.Sprintf("key%02dabc", i)] = fmt.Sprintf("value %d", i)
	}
	var group errgroup.Group
	for key, value := range want {
		group.Go(func() error {
			return fs.Put(t.Context(), key, uint64(len(value)), strings.NewReader(value))
		})
	}
	require.NoError(t, group.Wait())

	got := make(map[string]string)
	for key := range want {
		got[key] = get(t, fs, key)
	}
	require.Equal(t, want, got)
}

func TestSyncingStoreCreatesMissingParentDirectories(t *testing.T) {
	fs := newSyncingStore(t, filepath.Join(t.TempDir(), "a", "b", "store"))
	put(t, fs, "abcd", "value")
	require.Equal(t, "value", get(t, fs, "abcd"))
}

func TestSyncingStoreReopensAStoreItCreated(t *testing.T) {
	dir := t.TempDir()
	put(t, newSyncingStore(t, dir), "abcd", "value")
	require.Equal(t, "value", get(t, newSyncingStore(t, dir), "abcd"))
}

func TestSyncingStoreDeletesAnObject(t *testing.T) {
	fs := newSyncingStore(t, t.TempDir())
	put(t, fs, "abcd", "value")
	require.NoError(t, fs.Delete(t.Context(), "abcd"))
	_, err := fs.Get(t.Context(), "abcd")
	require.ErrorIs(t, err, objectstore.ErrNotExist)
}

func newSyncingStore(t *testing.T, dir string) *flatfs.Store {
	t.Helper()
	fs, err := flatfs.New(dir, flatfs.NextToLast(2), true)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fs.Close() })
	return fs
}

func put(t *testing.T, fs *flatfs.Store, key, value string) {
	t.Helper()
	require.NoError(t, fs.Put(t.Context(), key, uint64(len(value)), strings.NewReader(value)))
}

func get(t *testing.T, fs *flatfs.Store, key string) string {
	t.Helper()
	obj, err := fs.Get(t.Context(), key)
	require.NoError(t, err)
	data, err := io.ReadAll(obj.Body())
	require.NoError(t, err)
	return string(data)
}
