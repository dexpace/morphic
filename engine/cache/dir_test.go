// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/cache"
)

func TestDir_LoadReturnsWhatStoreKept(t *testing.T) {
	t.Parallel()
	dir := newDir(t, filepath.Join(t.TempDir(), "made", "by", "store"))
	key := keyN(0)

	_, ok, err := dir.Load(t.Context(), key)
	require.NoError(t, err, "a directory not yet made is an empty one")
	assert.False(t, ok)

	require.NoError(t, dir.Store(t.Context(), key, []byte("one")))
	got, ok, err := dir.Load(t.Context(), key)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "one", string(got))

	require.NoError(t, dir.Store(t.Context(), key, []byte("two")))
	got, _, err = dir.Load(t.Context(), key)
	require.NoError(t, err)
	assert.Equal(t, "two", string(got), "a second Store replaces the first")

	_, ok, err = dir.Load(t.Context(), keyN(1))
	require.NoError(t, err)
	assert.False(t, ok, "another key is another entry")
}

// TestDir_OverlappingCallsSeeAWholeEntry stores and loads one key from several
// Dirs, as several processes would, each writing a different entry of one length
// past the limit so pruning runs too. A Load sees one whole entry or none.
func TestDir_OverlappingCallsSeeAWholeEntry(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	const workers, rounds, size = 4, 25, 512
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Go(func() {
			dir := cache.NewDirLimited(path, size)
			entry := bytes.Repeat([]byte{byte('a' + worker)}, size)
			for round := range rounds {
				if !assert.NoError(t, dir.Store(t.Context(), keyN(round%2), entry)) {
					return
				}
				got, ok, err := dir.Load(t.Context(), keyN(round%2))
				if !assert.NoError(t, err) {
					return
				}
				if ok {
					assert.Equal(t, bytes.Repeat(got[:1], size), got, "a torn entry")
				}
			}
		})
	}
	wg.Wait()
}

func TestDir_NewDirectoryIsPrivate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cache")
	require.NoError(t, newDir(t, path).Store(t.Context(), keyN(0), []byte("x")))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o700), info.Mode().Perm())
}

// TestDir_LoadBounds holds Load at the limit: a file of exactly the limit reads,
// and one a byte past it is refused rather than returned cut.
func TestDir_LoadBounds(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	dir := cache.NewDirLimited(path, 4)
	aged(t, path, keyN(0), "1234", 0)
	aged(t, path, keyN(1), "12345", 0)

	got, ok, err := dir.Load(t.Context(), keyN(0))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "1234", string(got))

	_, ok, err = dir.Load(t.Context(), keyN(1))
	require.ErrorContains(t, err, "past the limit")
	assert.False(t, ok)
}

// TestDir_StoreBounds keeps an entry of exactly the limit and refuses one a byte
// past it, leaving what the directory held.
func TestDir_StoreBounds(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	dir := cache.NewDirLimited(path, 4)
	require.NoError(t, dir.Store(t.Context(), keyN(0), []byte("1234")))
	require.ErrorContains(t, dir.Store(t.Context(), keyN(1), []byte("12345")), "past the limit")

	_, ok, err := dir.Load(t.Context(), keyN(0))
	require.NoError(t, err)
	assert.True(t, ok)
	assert.NoFileExists(t, fileOf(path, keyN(1)))
}

func TestDir_LoadFailures(t *testing.T) {
	t.Parallel()
	t.Run("a directory where an entry would be", func(t *testing.T) {
		t.Parallel()
		path := t.TempDir()
		require.NoError(t, os.Mkdir(fileOf(path, keyN(0)), 0o700))
		_, ok, err := newDir(t, path).Load(t.Context(), keyN(0))
		require.Error(t, err)
		assert.False(t, ok)
	})
	t.Run("a cancelled context", func(t *testing.T) {
		t.Parallel()
		path := t.TempDir()
		aged(t, path, keyN(0), "x", 0)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, ok, err := newDir(t, path).Load(ctx, keyN(0))
		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, ok)
	})
}

// TestNewDir_RefusesNoPath holds what would otherwise read and write the working
// directory.
func TestNewDir_RefusesNoPath(t *testing.T) {
	t.Parallel()
	dir, err := cache.NewDir("")
	require.ErrorContains(t, err, "no directory")
	assert.Nil(t, dir)
}

// TestDir_TheZeroDirRefusesEveryCall holds the zero value to naming no
// directory, rather than the working directory.
func TestDir_TheZeroDirRefusesEveryCall(t *testing.T) {
	t.Parallel()
	var dir cache.Dir
	_, ok, err := dir.Load(t.Context(), keyN(0))
	require.ErrorContains(t, err, "no directory")
	assert.False(t, ok)
	require.ErrorContains(t, dir.Store(t.Context(), keyN(0), []byte("x")), "no directory")
}

func TestDir_StoreFailures(t *testing.T) {
	t.Parallel()
	t.Run("a file where the directory would be", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		require.Error(t, newDir(t, path).Store(t.Context(), keyN(0), []byte("x")))
	})
	t.Run("a cancelled context publishes nothing", func(t *testing.T) {
		t.Parallel()
		path := t.TempDir()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		require.ErrorIs(t, newDir(t, path).Store(ctx, keyN(0), []byte("x")), context.Canceled)
		assert.NoFileExists(t, fileOf(path, keyN(0)))
	})
}

// TestDir_LoadKeepsAnEntryInUse loads the oldest entry, then stores past the
// limit: the next oldest goes. Ordered by write time, the one entry every run
// hits would go first.
func TestDir_LoadKeepsAnEntryInUse(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	aged(t, path, keyN(0), "12", 3*time.Hour)
	aged(t, path, keyN(1), "12", 2*time.Hour)
	dir := cache.NewDirLimited(path, 4)
	_, ok, err := dir.Load(t.Context(), keyN(0))
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, dir.Store(t.Context(), keyN(2), []byte("12")))

	assert.FileExists(t, fileOf(path, keyN(0)), "the entry in use stays")
	assert.NoFileExists(t, fileOf(path, keyN(1)), "the entry unused longest goes")
}

// TestDir_AnUnmarkableEntryStillAnswers fails the mark, as a read-only directory
// or another user's entry does: the entry is sound, and only its place in line
// for eviction is lost.
func TestDir_AnUnmarkableEntryStillAnswers(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	aged(t, path, keyN(0), "entry", 0)
	dir := newDir(t, path)
	marked := 0
	cache.UseOps(dir, cache.Ops{Mark: func(string, time.Time, time.Time) error {
		marked++
		return fs.ErrPermission
	}})
	got, ok, err := dir.Load(t.Context(), keyN(0))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "entry", string(got))
	assert.Equal(t, 1, marked, "a fixture that reaches the mark")
}
