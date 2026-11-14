// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// bytesAbort is what bytesT.Fatalf panics with, so a test can tell the abort of
// a failed test from a fault in the helper under test.
type bytesAbort struct{}

// bytesT is a stub testingT that records what a golden compare reports. Fatalf
// aborts the compare by panicking with bytesAbort, which runOn recovers, so the
// code after the call does not run, as under *testing.T, with no goroutine.
// Both failures mark the stub failed, and failed can be set before the compare.
type bytesT struct {
	failed bool
	fatalf []string
	errorf []string
}

func (b *bytesT) Helper()      {}
func (b *bytesT) Failed() bool { return b.failed }

func (b *bytesT) Fatalf(format string, args ...any) {
	b.fatalf = append(b.fatalf, fmt.Sprintf(format, args...))
	b.failed = true
	panic(bytesAbort{})
}

func (b *bytesT) Errorf(format string, args ...any) {
	b.errorf = append(b.errorf, fmt.Sprintf(format, args...))
	b.failed = true
}

// runOn calls fn and reports whether it aborted through b.Fatalf.
func runOn(fn func()) (aborted bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, isAbort := r.(bytesAbort); !isAbort {
				panic(r)
			}
			aborted = true
		}
	}()
	fn()
	return false
}

// compareBytesOn runs the byte compare against b and reports whether it aborted.
func compareBytesOn(b *bytesT, path string, got []byte) bool {
	return runOn(func() { irtest.CompareBytesOn(b, path, got) })
}

// TestCompareBytes_UpdateRefusesAFailedTest pins that a test that has already
// failed rewrites nothing under -update: the compare aborts, naming the golden
// it left, and a golden already on disk keeps its bytes, where none is created.
func TestCompareBytes_UpdateRefusesAFailedTest(t *testing.T) {
	// Not parallel: toggles the shared -update flag.
	dir := t.TempDir()
	absent := filepath.Join(dir, "absent.golden")
	present := filepath.Join(dir, "present.golden")
	require.NoError(t, os.WriteFile(present, []byte("old\n"), 0o644))

	withUpdate(t, true, func() {
		for _, path := range []string{absent, present} {
			b := &bytesT{failed: true}
			require.True(t, compareBytesOn(b, path, []byte("regressed\n")), "a failed test aborts under -update")
			require.Len(t, b.fatalf, 1)
			assert.Contains(t, b.fatalf[0], "has already failed")
			assert.Contains(t, b.fatalf[0], path, "the refusal names the golden it left")
		}
	})

	_, err := os.Stat(absent)
	assert.ErrorIs(t, err, os.ErrNotExist, "a refused update creates no golden")
	kept, err := os.ReadFile(present)
	require.NoError(t, err)
	assert.Equal(t, "old\n", string(kept), "a refused update leaves the golden as it was")
}

// TestCompareBytes_UpdateWritesForAPassingTest pins that a test that has not
// failed rewrites the golden under -update, creating the directories above it.
func TestCompareBytes_UpdateWritesForAPassingTest(t *testing.T) {
	// Not parallel: toggles the shared -update flag.
	path := filepath.Join(t.TempDir(), "a", "b", "doc.golden")

	withUpdate(t, true, func() {
		b := &bytesT{}
		require.False(t, compareBytesOn(b, path, []byte("new\n")), "a passing test does not abort under -update")
		assert.Empty(t, b.fatalf)
		assert.Empty(t, b.errorf)
	})

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new\n", string(written), "the golden holds the bytes, parents created")

	withUpdate(t, true, func() {
		b := &bytesT{}
		require.False(t, compareBytesOn(b, path, []byte("newer\n")))
	})
	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "newer\n", string(rewritten), "an existing golden is rewritten")
}

// TestCompareBytes_UpdateReportsAWriteFailure pins that a golden that cannot be
// written aborts the test rather than passing: the path's parent is a file.
func TestCompareBytes_UpdateReportsAWriteFailure(t *testing.T) {
	// Not parallel: toggles the shared -update flag.
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	withUpdate(t, true, func() {
		b := &bytesT{}
		require.True(t, compareBytesOn(b, filepath.Join(blocker, "sub", "doc.golden"), []byte("x")))
		assert.NotEmpty(t, b.fatalf)
	})
}

// TestCompareBytes_ReadsAndDiffsWithoutUpdate pins the three outcomes of a
// compare: a missing golden aborts and says how to create it, a different one
// reports a diff with the golden's lines marked - and the got ones +, and an
// equal one reports nothing. A failed test still compares, since only a rewrite
// is refused.
func TestCompareBytes_ReadsAndDiffsWithoutUpdate(t *testing.T) {
	// Not parallel: reads the shared -update flag, which must be off.
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.golden")

	withUpdate(t, false, func() {
		missing := &bytesT{}
		require.True(t, compareBytesOn(missing, path, []byte("new\n")), "a missing golden aborts")
		require.Len(t, missing.fatalf, 1)
		assert.Contains(t, missing.fatalf[0], "read golden "+path)
		assert.Contains(t, missing.fatalf[0], "(run with -update to create)")

		require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o644))
		differing := &bytesT{failed: true}
		require.False(t, compareBytesOn(differing, path, []byte("new\n")), "a difference reports, it does not abort")
		require.Len(t, differing.errorf, 1)
		assert.Contains(t, differing.errorf[0], "golden mismatch for "+path+" (-golden +got); -update rewrites it:")
		assert.Regexp(t, `(?m)^\s*-.*old`, differing.errorf[0], "the golden's line is the - one")
		assert.Regexp(t, `(?m)^\s*\+.*new`, differing.errorf[0], "the got line is the + one")
		assert.NotRegexp(t, `(?m)^\s*-.*new`, differing.errorf[0])

		equal := &bytesT{failed: true}
		require.False(t, compareBytesOn(equal, path, []byte("old\n")))
		assert.Empty(t, equal.errorf)
		assert.Empty(t, equal.fatalf)
	})
	kept, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "old\n", string(kept), "a compare rewrites nothing")
}

// TestCompareBytes_RefusesAnEmptyPath pins that an empty path is refused with or
// without -update, before anything is read or written.
func TestCompareBytes_RefusesAnEmptyPath(t *testing.T) {
	// Not parallel: toggles the shared -update flag.
	for _, update := range []bool{false, true} {
		withUpdate(t, update, func() {
			b := &bytesT{}
			require.True(t, compareBytesOn(b, "", []byte("x")), "update=%v", update)
			require.Len(t, b.fatalf, 1)
			assert.Equal(t, "irtest: compare golden: empty golden path", b.fatalf[0])
		})
	}
}

// TestCompareBytes_PassesOnTheBytesOnDisk drives the exported helper with a
// real *testing.T: bytes equal to the golden's pass, as a first caller sees it.
func TestCompareBytes_PassesOnTheBytesOnDisk(t *testing.T) {
	// Not parallel: reads the shared -update flag, which must be off.
	path := filepath.Join(t.TempDir(), "doc.golden")
	require.NoError(t, os.WriteFile(path, []byte("same\n"), 0o644))
	withUpdate(t, false, func() { irtest.CompareBytes(t, path, []byte("same\n")) })
}

// TestCompareGolden_UpdateRefusesAFailedTest pins that the document compare
// reaches the same refusal: under -update a failed test leaves the golden
// as it was, since CompareGolden is the encoding plus the byte compare.
func TestCompareGolden_UpdateRefusesAFailedTest(t *testing.T) {
	// Not parallel: toggles the shared -update flag.
	path := filepath.Join(t.TempDir(), "doc.golden.json")
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o644))
	doc := &ir.Document{IRVersion: ir.IRVersion, Name: "regressed"}

	withUpdate(t, true, func() {
		b := &bytesT{failed: true}
		require.True(t, runOn(func() { irtest.CompareGoldenOn(b, path, doc) }), "a failed test aborts under -update")
		require.Len(t, b.fatalf, 1)
		assert.Contains(t, b.fatalf[0], "has already failed")
	})
	kept, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "{}\n", string(kept), "the golden is left as it was")
}
