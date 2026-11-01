// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir/irtest"
)

// raceListFile is the list `make race` runs under the race detector, relative
// to the repository root. The Makefile reads this same file.
const raceListFile = "scripts/race-packages.txt"

// raceSubtree is the suffix that makes a race list entry cover a directory's
// whole subtree, as it does in a go test pattern.
const raceSubtree = "/..."

// raceExempt names the directories that run code on a goroutine and are still
// off the race list, each with the reason. A directory belongs here only when
// no morphic code runs on more than one goroutine in it.
var raceExempt = map[string]string{
	"compilers/openapi/e2e": "its goroutine only puts a deadline on one compile, which runs on " +
		"that goroutine alone (amplification_test.go)",
	"compilers/openapi/internal/canary": "its goroutines drive child processes and the parser " +
		"library, not morphic code running concurrently; child timeouts under load are #1010",
	"ir/irtest": "each goroutine hosts a recording testing.T, so that FailNow's Goexit ends " +
		"that goroutine and not the test, and hands its result back over a channel",
}

// TestRaceList_NamesEveryConcurrentPackage holds scripts/race-packages.txt to
// the directories that run concurrently: each is listed or exempt in
// raceExempt with a reason, and no entry or exemption names a directory that
// does not. raceListViolations says what counts as concurrent.
func TestRaceList_NamesEveryConcurrentPackage(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	entries, err := readRaceList(filepath.Join(root, filepath.FromSlash(raceListFile)))
	require.NoError(t, err)

	violations, err := raceListViolations(root, entries, raceExempt)
	require.NoError(t, err)
	for _, v := range violations {
		t.Error(v)
	}
}

// TestParseRaceList_ReadsTheFormatTheMakefileReads feeds the reader one file it
// accepts and one for each shape it refuses, so the Makefile's sed and this
// reader cannot take the same file two ways.
func TestParseRaceList_ReadsTheFormatTheMakefileReads(t *testing.T) {
	t.Parallel()
	got, err := parseRaceList("# a comment\n\n   \n  # an indented comment\n./a\n./b/c/...\n./d\n")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b/c/...", "d"}, got)

	tests := []struct {
		name string
		src  string
		want string
	}{
		{"no entries", "# only a comment\n\n", "lists no package"},
		{"empty file", "", "lists no package"},
		{"duplicate", "./a\n./b\n./a\n", "line 3: duplicate"},
		{"no ./ prefix", "# c\na\n", "line 2: \"a\" does not start with"},
		{"inner whitespace", "./a b\n", "line 1: \"./a b\" holds whitespace"},
		{"trailing comment", "./a#x\n", "line 1: \"./a#x\" holds whitespace or '#'"},
		{"indented entry", "  ./a\n", "line 1: \"  ./a\" holds whitespace"},
		{"the root", "./\n", "line 1: \"./\" does not name a directory"},
		{"a dot segment", "./a/../b\n", "line 1: \"./a/../b\" does not name a directory"},
		{"a wildcard inside", "./a/.../b\n", "line 1: \"./a/.../b\" does not name a directory"},
		{"the whole module", "./...\n", "line 1: \"./...\" does not name a directory"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseRaceList(tc.src)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestReadRaceList_FailsOnAMissingFile holds that a list that cannot be read
// is an error, not an empty list that would flag every directory.
func TestReadRaceList_FailsOnAMissingFile(t *testing.T) {
	t.Parallel()
	_, err := readRaceList(filepath.Join(t.TempDir(), "absent.txt"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestRaceListViolations_PlantedShapesAreReported plants each shape the rule
// reports and each it leaves alone, and expects exactly the report.
func TestRaceListViolations_PlantedShapesAreReported(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Reported: production code that imports sync, on no list.
	writeDocFile(t, filepath.Join(root, "unlisted", "p.go"), "package p\n\nimport \"sync\"\n\nvar mu sync.Mutex\n")
	// Not reported: sync imported by a test file alone does not run a goroutine.
	writeDocFile(t, filepath.Join(root, "testonly", "p_test.go"), "package p\n\nimport \"sync\"\n\nvar mu sync.Mutex\n")
	// Not reported: one x/... entry covers the directory and its child.
	writeGoStmt(t, filepath.Join(root, "nested", "w_test.go"), "nested")
	writeGoStmt(t, filepath.Join(root, "nested", "child", "w_test.go"), "child")
	// Not reported: exempt, with a single-argument .Go call.
	writeNamedGoCall(t, filepath.Join(root, "exempt", "w_test.go"), "exempt")
	// Reported: an exemption for a directory that runs nothing concurrently.
	writeDocFile(t, filepath.Join(root, "calm", "p.go"), "package p\n")
	// Reported: a directory both listed and exempt.
	writeGoStmt(t, filepath.Join(root, "both", "w_test.go"), "both")
	// Not reported: the go tool does not read testdata.
	writeGoStmt(t, filepath.Join(root, "testdata", "w.go"), "fixture")
	writeGoStmt(t, filepath.Join(root, "nested", "testdata", "w.go"), "fixture")

	entries := []string{"nested/...", "both", "gone", "calm/..."}
	exempt := map[string]string{"exempt": "r", "calm": "r", "both": "r", "vanished": "r"}

	got, err := raceListViolations(root, entries, exempt)
	require.NoError(t, err)
	want := []string{
		`./calm/...: covers no package that runs concurrently`,
		`./gone: covers no package that runs concurrently`,
		`both: is on the race list and in raceExempt, which are exclusive`,
		`calm: is in raceExempt but runs nothing concurrently`,
		`unlisted: runs concurrently (unlisted/p.go imports "sync") but is neither on the race list ` +
			`nor in raceExempt`,
		`vanished: is in raceExempt but runs nothing concurrently`,
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("report differs (-want +got):\n%s", d)
	}
}

// TestRaceListViolations_NamesBothReasonsAndEveryFile holds that a directory
// flagged twice over reports each file with the reason it met, in path order.
func TestRaceListViolations_NamesBothReasonsAndEveryFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeNamedGoCall(t, filepath.Join(root, "d", "b_test.go"), "d")
	writeDocFile(t, filepath.Join(root, "d", "a.go"), "package d\n\nimport \"sync/atomic\"\n\nvar n atomic.Int64\n")
	writeGoStmt(t, filepath.Join(root, "d", "c.go"), "d")

	got, err := raceListViolations(root, []string{"d"}, nil)
	require.NoError(t, err)
	require.Empty(t, got, "a listed directory is not reported")

	got, err = raceListViolations(root, nil, nil)
	require.NoError(t, err)
	want := []string{`d: runs concurrently (d/a.go imports "sync/atomic", d/b_test.go starts a goroutine, ` +
		`d/c.go starts a goroutine) but is neither on the race list nor in raceExempt`}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("report differs (-want +got):\n%s", d)
	}
}

// TestRaceListViolations_RootDirectoryIsNamedDot holds that a file directly
// under the root is the directory ".", which an entry cannot name, so the rule
// reports it rather than skipping it.
func TestRaceListViolations_RootDirectoryIsNamedDot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeGoStmt(t, filepath.Join(root, "w_test.go"), "root")

	got, err := raceListViolations(root, nil, nil)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, strings.HasPrefix(got[0], ".: runs concurrently (w_test.go starts a goroutine)"), got[0])
}

// TestRaceListViolations_FailuresAreReturned holds that a file that does not
// parse ends the walk with an error rather than a clean report.
func TestRaceListViolations_FailuresAreReturned(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDocFile(t, filepath.Join(root, "p.go"), "package p\n\nfunc {")

	_, err := raceListViolations(root, nil, nil)
	require.Error(t, err)
}

// readRaceList reads the race list at file and returns its entries as
// parseRaceList does. It reads through os.ReadFile, which Go's test cache
// tracks, so editing the list re-runs the test that reads it; a child process
// reading the file would leave a stale pass cached.
func readRaceList(file string) ([]string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("reading the race list: %w", err)
	}
	return parseRaceList(string(data))
}

// parseRaceList returns the directories src names, each without its "./"
// prefix and keeping a trailing "/..." as the marker of a subtree. It reads
// the format the Makefile's sed reads: a line whose first non-blank character
// is "#" is a comment, a blank line is skipped, and anything else is one
// "./"-prefixed pattern with no whitespace and no "#". It refuses any other
// line, a duplicate entry and a list with no entry, naming the line.
func parseRaceList(src string) ([]string, error) {
	var entries []string
	seen := map[string]int{}
	for i, line := range strings.Split(src, "\n") {
		n := i + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		entry, err := raceEntry(line)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", raceListFile, n, err)
		}
		if first, dup := seen[entry]; dup {
			return nil, fmt.Errorf("%s line %d: duplicate of line %d", raceListFile, n, first)
		}
		seen[entry] = n
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s lists no package, so `make race` would run nothing", raceListFile)
	}
	return entries, nil
}

// raceEntry returns the directory a race list line names, keeping a trailing
// "/..." on it, or an error saying how the line is not a single "./"-prefixed
// pattern naming a directory below the root.
func raceEntry(line string) (string, error) {
	if strings.ContainsAny(line, " \t#") {
		return "", fmt.Errorf("%q holds whitespace or '#', which only a whole-line comment may", line)
	}
	rest, ok := strings.CutPrefix(line, "./")
	if !ok {
		return "", fmt.Errorf("%q does not start with \"./\"", line)
	}
	dir := strings.TrimSuffix(rest, raceSubtree)
	if dir == "" || dir == "." || !filepath.IsLocal(dir) || dir != path.Clean(dir) || strings.Contains(dir, "...") {
		return "", fmt.Errorf("%q does not name a directory below the root", line)
	}
	return rest, nil
}

// concurrentDirs returns, for each directory under root that runs code on more
// than one goroutine, the files that show it and the reason each met. Two
// things count: a non-test file importing "sync" or "sync/atomic", and any
// file, test or not, that startsGoroutine recognizes. A test file importing
// "sync" alone does not, nor does t.Parallel: tests that run one goroutine of
// morphic code are the race checking the list gives up. Directories are keyed
// by path relative to root, slash-separated, "." for root itself.
func concurrentDirs(root string) (map[string][]string, error) {
	flags := map[string][]string{}
	err := walkGoFiles(root, func(rel string, _ *token.FileSet, f *ast.File) {
		dir := path.Dir(rel)
		if !strings.HasSuffix(rel, "_test.go") {
			if name := importsSync(f); name != "" {
				flags[dir] = append(flags[dir], fmt.Sprintf("%s imports %q", rel, name))
			}
		}
		if startsGoroutine(f) {
			flags[dir] = append(flags[dir], rel+" starts a goroutine")
		}
	})
	if err != nil {
		return nil, err
	}
	for _, reasons := range flags {
		slices.Sort(reasons)
	}
	return flags, nil
}

// importsSync returns the first of "sync" and "sync/atomic" that f imports, or
// "" when it imports neither.
func importsSync(f *ast.File) string {
	for _, imp := range f.Imports {
		switch p := strings.Trim(imp.Path.Value, `"`); p {
		case "sync", "sync/atomic":
			return p
		default:
		}
	}
	return ""
}

// raceCovers reports whether a race list entry covers dir: the entry is dir
// itself, or is "p/..." with dir either p or below it.
func raceCovers(entry, dir string) bool {
	if entry == dir {
		return true
	}
	p, subtree := strings.CutSuffix(entry, raceSubtree)
	return subtree && (dir == p || strings.HasPrefix(dir, p+"/"))
}

// raceListViolations returns, sorted, one message for each way the race list
// and exempt (directory to reason) disagree with the directories under root
// that run concurrently (concurrentDirs): one that runs concurrently and is
// neither covered by an entry nor exempt; an entry that covers no such
// directory, a directory the entry does not name included; an exemption for a
// directory that does not run concurrently; and one that is both covered and
// exempt. Paths are relative to root.
func raceListViolations(root string, entries []string, exempt map[string]string) ([]string, error) {
	flagged, err := concurrentDirs(root)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, dir := range slices.Sorted(maps.Keys(flagged)) {
		covered := slices.ContainsFunc(entries, func(e string) bool { return raceCovers(e, dir) })
		_, isExempt := exempt[dir]
		switch {
		case covered && isExempt:
			out = append(out, dir+": is on the race list and in raceExempt, which are exclusive")
		case !covered && !isExempt:
			out = append(out, fmt.Sprintf(
				"%s: runs concurrently (%s) but is neither on the race list nor in raceExempt",
				dir, strings.Join(flagged[dir], ", ")))
		}
	}
	for _, e := range entries {
		if !slices.ContainsFunc(slices.Collect(maps.Keys(flagged)), func(dir string) bool { return raceCovers(e, dir) }) {
			out = append(out, "./"+e+": covers no package that runs concurrently")
		}
	}
	for dir := range exempt {
		if _, ok := flagged[dir]; !ok {
			out = append(out, dir+": is in raceExempt but runs nothing concurrently")
		}
	}
	slices.Sort(out)
	return out, nil
}
