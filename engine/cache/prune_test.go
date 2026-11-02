// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/cache"
)

// TestDir_StoreRemovesTheOldestPastTheLimit keeps the entries that fit, newest
// first, and removes no more than it must.
func TestDir_StoreRemovesTheOldestPastTheLimit(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	aged(t, path, keyN(0), "12", 3*time.Hour)
	aged(t, path, keyN(1), "12", 2*time.Hour)
	aged(t, path, keyN(2), "12", time.Hour)

	require.NoError(t, cache.NewDirLimited(path, 6).Store(t.Context(), keyN(3), []byte("12")))

	assert.NoFileExists(t, fileOf(path, keyN(0)), "the oldest goes")
	assert.FileExists(t, fileOf(path, keyN(1)), "the rest fit exactly, so nothing more goes")
	assert.FileExists(t, fileOf(path, keyN(2)))
	assert.FileExists(t, fileOf(path, keyN(3)), "what was just stored stays")
}

// TestDir_StoreLeavesWhatItDidNotName puts a directory over its limit among
// files only a loose match would take for its own: a directory is often
// shared, and evicting must never reach a file the cache did not write.
func TestDir_StoreLeavesWhatItDidNotName(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	hexName := keyN(0).String()
	foreign := []string{
		"openapi.yaml",
		"notes.entry",
		hexName,
		hexName + ".entry.bak",
		hexName + ".entryx",
		hexName + "0.entry",
		hexName[1:] + ".entry",
		strings.ToUpper(hexName) + ".entry",
		"." + hexName + ".entry.tmp",
		".." + hexName + ".entry",
	}
	for _, name := range foreign {
		require.NoError(t, os.WriteFile(filepath.Join(path, name), []byte("xxxxxxxx"), 0o600))
		when := time.Now().Add(-time.Hour)
		require.NoError(t, os.Chtimes(filepath.Join(path, name), when, when))
	}
	aged(t, path, keyN(1), "xxxxxxxx", time.Hour)

	require.NoError(t, cache.NewDirLimited(path, 4).Store(t.Context(), keyN(2), []byte("1234")))

	for _, name := range foreign {
		assert.FileExists(t, filepath.Join(path, name), "a file the cache did not name")
	}
	assert.NoFileExists(t, fileOf(path, keyN(1)), "its own past entry goes")
}

// TestDir_StoreClearsWhatAnInterruptedStoreLeft holds a killed store's temporary
// file to the limit, which nothing else would ever remove.
func TestDir_StoreClearsWhatAnInterruptedStoreLeft(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	left := temporary(t, path, keyN(0), 2*time.Hour)

	require.NoError(t, cache.NewDirLimited(path, 4).Store(t.Context(), keyN(1), []byte("1234")))

	assert.NoFileExists(t, left)
	assert.FileExists(t, fileOf(path, keyN(1)))
}

// TestDir_StoreLeavesAStoreInProgress holds what removing a recent temporary
// would break: another process's rename. It sits past the limit, and is older
// than the entry that would otherwise go first, so only its age keeps it, and it
// must not count against the entries either.
func TestDir_StoreLeavesAStoreInProgress(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	live := temporary(t, path, keyN(0), 30*time.Minute)
	aged(t, path, keyN(1), "12", time.Minute)

	require.NoError(t, cache.NewDirLimited(path, 4).Store(t.Context(), keyN(2), []byte("12")))

	assert.FileExists(t, live)
	assert.FileExists(t, fileOf(path, keyN(1)), "the entries fit once it is left out")
	assert.FileExists(t, fileOf(path, keyN(2)))
}

func TestPrune_Failures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	own := func(n int) string { return keyN(n).String() + ".entry" }
	keep := func(string) error { return nil }
	failing := func(limit int, ops cache.Ops) error {
		dir := cache.NewDirLimited("d", limit)
		cache.UseOps(dir, ops)
		return cache.Prune(dir)
	}

	t.Run("a directory that will not list", func(t *testing.T) {
		t.Parallel()
		list := func(string) ([]fs.DirEntry, error) { return nil, boom }
		require.ErrorIs(t, failing(1, cache.Ops{List: list, Remove: keep}), boom)
	})
	t.Run("a file that will not stat", func(t *testing.T) {
		t.Parallel()
		list := listing(listedEntry{name: own(0), infoErr: boom})
		require.ErrorIs(t, failing(1, cache.Ops{List: list, Remove: keep}), boom)
	})
	t.Run("a file another process removed is no failure", func(t *testing.T) {
		t.Parallel()
		gone := listedEntry{name: own(0), size: 9, infoErr: fs.ErrNotExist}
		remove := func(string) error { return boom }
		require.NoError(t, failing(1, cache.Ops{List: listing(gone), Remove: remove}),
			"it counts for nothing, so nothing is removed")
	})
	t.Run("a file that will not go", func(t *testing.T) {
		t.Parallel()
		list := listing(listedEntry{name: own(0), size: 9})
		require.ErrorIs(t, failing(1, cache.Ops{List: list, Remove: func(string) error { return boom }}), boom)
	})
	t.Run("a file another process removed first still counts as gone", func(t *testing.T) {
		t.Parallel()
		var removed []string
		remove := func(name string) error {
			removed = append(removed, filepath.Base(name))
			return fs.ErrNotExist
		}
		files := listing(listedEntry{name: own(0), size: 9}, listedEntry{name: own(1), size: 9})
		require.NoError(t, failing(9, cache.Ops{List: files, Remove: remove}))
		assert.Len(t, removed, 1, "one removal brings the rest under the limit")
	})
}
