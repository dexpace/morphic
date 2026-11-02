// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/cache"
)

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
