// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/exectest"
	"github.com/dexpace/morphic/ir/irtest"
)

// eolProbes are paths whose line endings the golden comparison depends on: an IR
// snapshot, the spec it was compiled from, and a Go source file standing for the
// rest of the tree. Each is required to exist, so a rename cannot quietly turn
// the checks below into assertions about nothing.
var eolProbes = []string{
	"testdata/golden/openapi/petstore.golden.json",
	"testdata/conformance/openapi/named-types.yaml",
	"ir/irtest/golden.go",
}

// TestGitAttributes_PinsLineEndings asserts the repository carries the
// .gitattributes that keeps CompareGolden's byte comparison meaningful.
//
// CompareGolden compares the file on disk against JSON encodeGolden always
// writes with "\n", so a checkout that rewrote the fixtures to CRLF fails every
// golden and conformance test with a whole-file diff unrelated to the IR — the
// default on Windows, where core.autocrlf=true. Nothing else in the suite would
// notice the pin's removal: CI is Linux-only and checks out LF regardless
// (GitHub #48).
func TestGitAttributes_PinsLineEndings(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	require.NoError(t, err, ".gitattributes must exist at the repository root")

	var pins []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			pins = append(pins, trimmed)
		}
	}
	require.NotEmpty(t, pins, ".gitattributes carries only comments")
	assert.Contains(t, strings.Join(pins, "\n"), "eol=lf",
		".gitattributes must pin line endings to LF, got rules %q", pins)
}

// TestGitAttributes_ResolveToLF asks git what it would check the fixtures out
// as, rather than reading the patterns and reasoning about them. It is the half
// that survives a reformulation of the rules: any set of patterns resolving
// eol=lf for these paths passes, and any that leaves one unpinned fails however
// it is written.
func TestGitAttributes_ResolveToLF(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	for _, rel := range eolProbes {
		require.FileExists(t, filepath.Join(root, rel))
	}

	args := append([]string{"check-attr", "eol", "--"}, eolProbes...)
	cmd := exectest.Command(t.Context(), t, nil, "git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("git check-attr unavailable (%v); the pin itself is checked by TestGitAttributes_PinsLineEndings", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.Len(t, lines, len(eolProbes), "git reports one line per queried path")
	for i, line := range lines {
		assert.Equal(t, eolProbes[i]+": eol: lf", line,
			"git must check %s out with LF endings", eolProbes[i])
	}
}

// TestTrackedFiles_HoldNoCarriageReturn checks the working tree the tests
// actually read, which the two above do not: `.gitattributes` governs new
// checkouts, and a clone taken before it landed keeps its CRLF files until they
// are renormalized. That checkout fails the golden and conformance suites with
// whole-file diffs, and the diff says the IR changed when the line endings did.
//
// It reddens only where the problem exists. CI's checkout normalizes text
// files, so there it takes a CRLF blob committed with its filters bypassed, or a
// binary fixture holding 0x0D, to fail it — the same shape as the pin it
// accompanies.
func TestTrackedFiles_HoldNoCarriageReturn(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	cmd := exectest.Command(t.Context(), t, nil, "git", "ls-files", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("git ls-files unavailable (%v)", err)
	}

	require.NotEmpty(t, out, "git tracks no files, so an empty result proves nothing")
	var offenders []string
	tracked := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	dirs := map[string]bool{}
	for _, rel := range tracked {
		if _, statErr := os.Lstat(filepath.Join(root, rel)); statErr == nil {
			for dir := filepath.Dir(rel); !dirs[dir]; dir = filepath.Dir(dir) {
				dirs[dir] = true
			}
		}
		raw, readErr := os.ReadFile(filepath.Join(root, rel))
		if readErr != nil {
			continue // a file deleted but not staged, a submodule, or a link to no readable file
		}
		if bytes.ContainsRune(raw, '\r') {
			offenders = append(offenders, rel)
		}
	}
	listDirectories(t, root, dirs)
	assert.Empty(t, offenders,
		"these tracked files hold CR bytes; run `git add --renormalize .` to bring the checkout "+
			"back to LF, or mark a file that needs them `-text` in .gitattributes")
}

// listDirectories lists each directory in dirs, which are relative to root.
//
// go test keys a cached result on what the process opens, and a file opened
// inside the module is checked by its size, mode and time, which the Makefile's
// content-dating target sets alike on every checkout. A file git lists for the
// first time is not opened yet, so only a listing of its directory changes what
// the log holds and reruns the test.
// The files opened here include every nested .gitattributes, so this covers
// what TestGitAttributes_ResolveToLF asks git about as well.
func listDirectories(t *testing.T, root string, dirs map[string]bool) {
	t.Helper()
	require.NotEmpty(t, dirs, "no directory above a tracked file, so nothing was listed")
	for dir := range dirs {
		_, err := os.ReadDir(filepath.Join(root, dir))
		require.NoError(t, err, "listing %s", dir)
	}
}

// TestTrackedFiles_ListsEveryDirectoryAboveAFile holds listDirectories to its
// purpose through the test log, which is what go test keys its cached result
// on: the child run of the test above must open the root and every directory
// above a tracked file. A listing of the root alone, or of each file's parent
// alone, returns the right verdict on today's tree and misses a file added in
// a directory it never listed.
func TestTrackedFiles_ListsEveryDirectoryAboveAFile(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	cmd := exectest.Command(t.Context(), t, nil, "git", "ls-files", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("git ls-files unavailable (%v)", err)
	}

	want := map[string]bool{root: true}
	for rel := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if _, statErr := os.Lstat(filepath.Join(root, rel)); statErr != nil {
			continue // a file deleted but not staged, which the child skips too
		}
		parts := strings.Split(rel, "/")
		for n := 1; n < len(parts); n++ {
			want[filepath.Join(root, filepath.Join(parts[:n]...))] = true
		}
	}
	require.Greater(t, len(want), 1, "no directory below the root, so the walk above proves nothing")

	irtest.RequireOpens(t, "TestTrackedFiles_HoldNoCarriageReturn", slices.Sorted(maps.Keys(want))...)
}
