// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/cache"
)

// keyN is a key that differs for each n.
func keyN(n int) cache.Key { return cache.KeyOf("entry", string(rune('a'+n))) }

// fileOf is the file a Dir at dir holds key's entry in.
func fileOf(dir string, key cache.Key) string { return filepath.Join(dir, key.String()+".entry") }

// aged writes data as key's entry, last changed age ago.
func aged(t *testing.T, dir string, key cache.Key, data string, age time.Duration) {
	t.Helper()
	path := fileOf(dir, key)
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
	when := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, when, when))
}

func TestKeyOf_SpellsAHashOfItsFields(t *testing.T) {
	t.Parallel()
	key := cache.KeyOf("a", "b")
	assert.Len(t, key.String(), hex.EncodedLen(sha256.Size))
	assert.Equal(t, strings.ToLower(key.String()), key.String())
	assert.Equal(t, key, cache.KeyOf("a", "b"), "a key depends on nothing but its fields")
}

// TestKeyOf_ADifferentSequenceIsADifferentKey moves bytes across each boundary,
// which is what a key built by joining fields would miss.
func TestKeyOf_ADifferentSequenceIsADifferentKey(t *testing.T) {
	t.Parallel()
	sequences := [][]string{
		{},
		{""},
		{"", ""},
		{"ab"},
		{"a", "b"},
		{"b", "a"},
		{"ab", ""},
		{"", "ab"},
		{"a", "", "b"},
		{"\x01a"},
		{"\x01", "a"},
	}
	seen := map[cache.Key][]string{}
	for _, fields := range sequences {
		key := cache.KeyOf(fields...)
		other, taken := seen[key]
		assert.False(t, taken, "%q and %q share a key", fields, other)
		seen[key] = fields
	}
}

func TestDir_LoadReturnsWhatStoreKept(t *testing.T) {
	t.Parallel()
	dir := cache.NewDir(filepath.Join(t.TempDir(), "made", "by", "store"))
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
	require.NoError(t, cache.NewDir(path).Store(t.Context(), keyN(0), []byte("x")))
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
		_, ok, err := cache.NewDir(path).Load(t.Context(), keyN(0))
		require.Error(t, err)
		assert.False(t, ok)
	})
	t.Run("a cancelled context", func(t *testing.T) {
		t.Parallel()
		path := t.TempDir()
		aged(t, path, keyN(0), "x", 0)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, ok, err := cache.NewDir(path).Load(ctx, keyN(0))
		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, ok)
	})
}

// TestDir_NamingNoDirectoryIsRefused holds what would otherwise read and write
// the working directory, for an empty path and for the zero Dir alike.
func TestDir_NamingNoDirectoryIsRefused(t *testing.T) {
	t.Parallel()
	for name, dir := range map[string]*cache.Dir{"an empty path": cache.NewDir(""), "the zero Dir": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ok, err := dir.Load(t.Context(), keyN(0))
			require.ErrorContains(t, err, "no directory")
			assert.False(t, ok)
			require.ErrorContains(t, dir.Store(t.Context(), keyN(0), []byte("x")), "no directory")
		})
	}
}

func TestDir_StoreFailures(t *testing.T) {
	t.Parallel()
	t.Run("a file where the directory would be", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		require.Error(t, cache.NewDir(path).Store(t.Context(), keyN(0), []byte("x")))
	})
	t.Run("a cancelled context publishes nothing", func(t *testing.T) {
		t.Parallel()
		path := t.TempDir()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		require.ErrorIs(t, cache.NewDir(path).Store(ctx, keyN(0), []byte("x")), context.Canceled)
		assert.NoFileExists(t, fileOf(path, keyN(0)))
	})
}

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
	dir := cache.NewDir(path)
	marked := 0
	cache.MarkWith(dir, func(string, time.Time, time.Time) error {
		marked++
		return fs.ErrPermission
	})
	got, ok, err := dir.Load(t.Context(), keyN(0))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "entry", string(got))
	assert.Equal(t, 1, marked, "a fixture that reaches the mark")
}

// TestDir_StorePrunesOnceAnEighthIsStored starts over the limit with no ledger,
// as a directory whose limit fell would. Pruning waits until the Stores since the
// last pass an eighth of the limit, then runs and starts the ledger afresh.
func TestDir_StorePrunesOnceAnEighthIsStored(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	ledger := filepath.Join(path, cache.LedgerName)
	aged(t, path, keyN(0), strings.Repeat("x", 500), 3*time.Hour)
	aged(t, path, keyN(1), strings.Repeat("x", 500), 2*time.Hour)
	dir := cache.NewDirLimited(path, 800)

	for n := 2; n <= 3; n++ {
		require.NoError(t, dir.Store(t.Context(), keyN(n), bytes.Repeat([]byte("y"), 50)))
	}
	assert.FileExists(t, fileOf(path, keyN(0)), "100 bytes stored is not past an eighth of 800")

	require.NoError(t, dir.Store(t.Context(), keyN(4), []byte("z")))
	assert.NoFileExists(t, fileOf(path, keyN(0)), "101 is, and the oldest goes")
	assert.FileExists(t, fileOf(path, keyN(1)), "and only as much as the limit asks")
	assert.NoFileExists(t, ledger, "a prune starts the tally afresh")
}

// TestDir_ALedgerItCannotReadPrunes holds the guard to falling back: each ledger
// here holds less than an eighth stored, or nothing a reader can trust, and the
// directory is over its limit, so only a prune taking it as due passes.
func TestDir_ALedgerItCannotReadPrunes(t *testing.T) {
	t.Parallel()
	zero := fmt.Sprintf("%020d\n", 0)
	for name, write := range map[string]func(ledger string) error{
		"garbage":               func(l string) error { return os.WriteFile(l, []byte("garbage\n"), 0o600) },
		"a record cut short":    func(l string) error { return os.WriteFile(l, []byte(zero[:7]), 0o600) },
		"a size past the limit": func(l string) error { return os.WriteFile(l, fmt.Appendf(nil, "%020d\n", 1<<40), 0o600) },
		"a record of no number": func(l string) error { return os.WriteFile(l, []byte(strings.Repeat("x", 20)+"\n"), 0o600) },
		"sizes whose sum overflows": func(l string) error {
			return os.WriteFile(l, []byte(strings.Repeat(fmt.Sprintf("%020d\n", int64(1)<<62), 2)), 0o600)
		},
		"a negative size":         func(l string) error { return os.WriteFile(l, fmt.Appendf(nil, "-%019d\n", 1), 0o600) },
		"a record ending wrongly": func(l string) error { return os.WriteFile(l, []byte(zero[:20]+"x"), 0o600) },
		"a directory holding a file": func(l string) error {
			return errors.Join(os.Mkdir(l, 0o700), os.WriteFile(filepath.Join(l, "f"), nil, 0o600))
		},
		"every record allowed": func(l string) error {
			return os.WriteFile(l, []byte(strings.Repeat(zero, cache.MaxRecords)), 0o600)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := t.TempDir()
			aged(t, path, keyN(0), strings.Repeat("x", 900), time.Hour)
			require.NoError(t, write(filepath.Join(path, cache.LedgerName)))

			err := cache.NewDirLimited(path, 800).Store(t.Context(), keyN(1), []byte("y"))
			if name == "a directory holding a file" {
				require.ErrorContains(t, err, "reset the ledger", "a ledger it cannot remove is reported")
			} else {
				require.NoError(t, err)
			}
			assert.NoFileExists(t, fileOf(path, keyN(0)))
		})
	}
}

// TestTallyDue_ReadsOnlyWholeRecordsWithinTheBound hands the tally ledgers a Store
// cannot make, its own record always being whole and last: a read caught mid-write
// and one past the bound, each beside a control one step short of it.
func TestTallyDue_ReadsOnlyWholeRecordsWithinTheBound(t *testing.T) {
	t.Parallel()
	record := func(n int) string { return fmt.Sprintf("%020d\n", n) }
	for _, tc := range []struct {
		name   string
		ledger string
		due    bool
	}{
		{"nothing", "", false},
		{"part of a record", record(1)[:7], true},
		{"a record and part of another", record(1) + record(1)[:20], true},
		{"one record under an eighth", record(100), false},
		{"one record past an eighth", record(101), true},
		{"every record allowed", strings.Repeat(record(0), cache.MaxRecords), false},
		{"one past every record allowed", strings.Repeat(record(0), cache.MaxRecords+1), true},
	} {
		assert.Equal(t, tc.due, cache.TallyDue([]byte(tc.ledger), 800), tc.name)
	}
}

// TestDir_ALedgerItCannotReadIsDue writes the ledger and fails to read it back,
// as a ledger of mode 0200 does. The directory is over its limit, so only a
// prune taking it as due passes. Root reads any file, so it cannot be run there.
func TestDir_ALedgerItCannotReadIsDue(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root reads a file of mode 0200")
	}
	path := t.TempDir()
	aged(t, path, keyN(0), strings.Repeat("x", 900), time.Hour)
	require.NoError(t, os.WriteFile(filepath.Join(path, cache.LedgerName), nil, 0o200))

	require.NoError(t, cache.NewDirLimited(path, 800).Store(t.Context(), keyN(1), []byte("y")))
	assert.NoFileExists(t, fileOf(path, keyN(0)))
}

// TestDir_ALedgerJustShortOfFullWaits is the control for the last row above: one
// record fewer and no prune is due, so that row reaches the bound on records.
func TestDir_ALedgerJustShortOfFullWaits(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	aged(t, path, keyN(0), strings.Repeat("x", 900), time.Hour)
	zero := fmt.Sprintf("%020d\n", 0)
	ledger := filepath.Join(path, cache.LedgerName)
	require.NoError(t, os.WriteFile(ledger, []byte(strings.Repeat(zero, cache.MaxRecords-1)), 0o600))

	require.NoError(t, cache.NewDirLimited(path, 800).Store(t.Context(), keyN(1), []byte("y")))
	assert.FileExists(t, fileOf(path, keyN(0)))
}

// temporary writes the file a Store of key would rename into place, last
// changed age ago.
func temporary(t *testing.T, path string, key cache.Key, age time.Duration) string {
	t.Helper()
	name := filepath.Join(path, "."+key.String()+".entry1234567890123456789")
	require.NoError(t, os.WriteFile(name, []byte("xxxxxxxx"), 0o600))
	when := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(name, when, when))
	return name
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

// listedEntry is a directory entry whose Info answers as a test says.
type listedEntry struct {
	name    string
	size    int64
	infoErr error
}

func (e listedEntry) Name() string               { return e.name }
func (listedEntry) IsDir() bool                  { return false }
func (listedEntry) Type() fs.FileMode            { return 0 }
func (e listedEntry) Info() (fs.FileInfo, error) { return listedInfo(e), e.infoErr }

// listedInfo is the fs.FileInfo of a listedEntry.
type listedInfo listedEntry

func (i listedInfo) Name() string     { return i.name }
func (i listedInfo) Size() int64      { return i.size }
func (listedInfo) Mode() fs.FileMode  { return 0 }
func (listedInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (listedInfo) IsDir() bool        { return false }
func (listedInfo) Sys() any           { return nil }

func listing(entries ...fs.DirEntry) func(string) ([]fs.DirEntry, error) {
	return func(string) ([]fs.DirEntry, error) { return entries, nil }
}

func TestPrune_Failures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	own := func(n int) string { return keyN(n).String() + ".entry" }
	keep := func(string) error { return nil }

	t.Run("a directory that will not list", func(t *testing.T) {
		t.Parallel()
		list := func(string) ([]fs.DirEntry, error) { return nil, boom }
		require.ErrorIs(t, cache.Prune("d", 1, list, keep), boom)
	})
	t.Run("a file that will not stat", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, cache.Prune("d", 1, listing(listedEntry{name: own(0), infoErr: boom}), keep), boom)
	})
	t.Run("a file another process removed is no failure", func(t *testing.T) {
		t.Parallel()
		gone := listedEntry{name: own(0), size: 9, infoErr: fs.ErrNotExist}
		require.NoError(t, cache.Prune("d", 1, listing(gone), func(string) error { return boom }),
			"it counts for nothing, so nothing is removed")
	})
	t.Run("a file that will not go", func(t *testing.T) {
		t.Parallel()
		err := cache.Prune("d", 1, listing(listedEntry{name: own(0), size: 9}), func(string) error { return boom })
		require.ErrorIs(t, err, boom)
	})
	t.Run("a file another process removed first still counts as gone", func(t *testing.T) {
		t.Parallel()
		var removed []string
		remove := func(name string) error {
			removed = append(removed, filepath.Base(name))
			return fs.ErrNotExist
		}
		files := listing(listedEntry{name: own(0), size: 9}, listedEntry{name: own(1), size: 9})
		require.NoError(t, cache.Prune("d", 9, files, remove))
		assert.Len(t, removed, 1, "one removal brings the rest under the limit")
	})
}

func TestBuildID_NamesTheExecutable(t *testing.T) {
	t.Parallel()
	id, err := cache.BuildID(t.Context())
	require.NoError(t, err)
	assert.Len(t, id, hex.EncodedLen(sha256.Size))

	dir := t.TempDir()
	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	require.NoError(t, os.WriteFile(first, []byte("one build"), 0o600))
	require.NoError(t, os.WriteFile(second, []byte("another build"), 0o600))
	at := func(path string) func() (string, error) { return func() (string, error) { return path, nil } }

	a, err := cache.BuildIDOf(t.Context(), at(first))
	require.NoError(t, err)
	again, err := cache.BuildIDOf(t.Context(), at(first))
	require.NoError(t, err)
	b, err := cache.BuildIDOf(t.Context(), at(second))
	require.NoError(t, err)
	assert.Equal(t, a, again)
	assert.NotEqual(t, a, b, "two executables are two builds")
}

func TestBuildID_Failures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	_, err := cache.BuildIDOf(t.Context(), func() (string, error) { return "", boom })
	require.ErrorIs(t, err, boom)

	missing := filepath.Join(t.TempDir(), "gone")
	_, err = cache.BuildIDOf(t.Context(), func() (string, error) { return missing, nil })
	require.ErrorIs(t, err, fs.ErrNotExist)
}
