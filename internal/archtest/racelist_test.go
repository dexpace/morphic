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
	"strconv"
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

// raceRootsFile names the trees `make race-pipeline` adds to the list, relative
// to the repository root. The Makefile reads this same file.
const raceRootsFile = "scripts/race-pipeline-roots.txt"

// raceSubtree is the suffix that makes a race list entry cover a directory's
// whole subtree, as it does in a go test pattern.
const raceSubtree = "/..."

// raceRule is what the two race runs are held to. roots are the pipeline trees
// of raceRootsFile, each without its "/...", and entries the list of
// raceListFile. Every package under a root is raced by `make race-pipeline`, so
// none may be in exempt; pipelineOnly names the ones with a narrow signal that
// `make race` leaves to it.
type raceRule struct {
	roots        []string
	entries      []string
	exempt       map[string]string
	pipelineOnly map[string]string
}

// raceExempt names the directories that run code concurrently and are
// deliberately off both race runs, each with the reason. All of them are
// tooling outside the pipeline, and no directory under a root of
// raceRootsFile may be here. A race on a path that only an exempt package's own
// tests take is found by neither `make race` nor `make race-pipeline`.
var raceExempt = map[string]string{
	"cmd/morphic-census": "tooling outside the pipeline: its parallel tests call run on separate " +
		"goroutines, each compiling specs through the engine into buffers of its own",
	"cmd/morphic-covergate": "tooling outside the pipeline: its parallel tests call run over " +
		"coverage profiles they write to their own temporary files, or over arguments that name " +
		"none, and tallyOf over profiles they build in memory",
	"cmd/morphic-filetimes": "tooling outside the pipeline: its parallel tests run execute, which " +
		"shells out to git, each in a temporary repository of its own, and call its helpers over " +
		"values held in memory",
	"cmd/morphic-harness": "tooling outside the pipeline: its parallel tests call run on separate " +
		"goroutines, each sweeping a directory of its own through harness.CheckPath",
	"cmd/morphic-vet": "tooling outside the pipeline: its one parallel test starts the test " +
		"binary as a child process (main_test.go), so no morphic code of its own runs twice",
	"internal/archtest": "tooling outside the pipeline: its parallel tests read the module's " +
		"sources and type-check them through go/packages, whose go/types race is #855",
	"internal/census": "tooling outside the pipeline: #1271 left it off, and its parallel tests " +
		"call (*Census).Add and Report, each on a Census of its own",
	"internal/conformance": "tooling outside the pipeline: its parallel tests read the " +
		"conformance matrix and corpus as text and ask engine.New which formats it registers; " +
		"they compile no spec",
	"internal/decls": "tooling outside the pipeline: its parallel tests call Load, which " +
		"type-checks packages through go/packages, each in a load of its own over fixtures they only read",
	"internal/exectest": "test infrastructure outside the pipeline: its parallel tests build " +
		"commands with Command, read their environments and refuse bad arguments, and one calls " +
		"Run on a cancelled /bin/sh, which starts no child",
	"internal/harness": "tooling outside the pipeline: #1271 left it off, and its parallel tests " +
		"call harness.Check on parallel goroutines",
	"internal/jsonshape": "tooling outside the pipeline: its parallel tests call Describe and " +
		"Read over fixture packages that internal/decls loads",
	"internal/keydoc": "tooling outside the pipeline: its parallel tests parse Markdown and " +
		"comments held in memory, and walk temporary trees of their own",
	"internal/lintrules": "tooling outside the pipeline: its parallel tests run the analyzers " +
		"over planted packages that go/packages loads",
}

// racePipelineOnly names the pipeline directories whose goroutine or sync
// import `make race` leaves off its list, each with the reason. They are not
// exempt: `make race-pipeline` races them with every other package under a
// root, so a race only their own tests reach is found after merge, by the run
// of that target each push to main makes, and not on the pull request. It is a
// set of deferrals, not a second list of targets.
var racePipelineOnly = map[string]string{
	"compilers/openapi/e2e": "its two goroutines each run one Compile while the test waits under " +
		"a deadline (amplification_test.go), adding nothing to its parallel tests, which run " +
		"Compile on many goroutines and are raced by make race-pipeline",
	"compilers/openapi/internal/canary": "its goroutines run no pipeline code in the test " +
		"process: panicElsewhere and watchMemory run in a re-run of the test binary, " +
		"goroutinesAlive decodes one document on one goroutine, and the oracle's shards only " +
		"drive children and merge their answers under a mutex; its parallel tests are raced by " +
		"make race-pipeline, and child timeouts under load are #1010",
	"ir/irtest": "each goroutine hosts a recording testing.T, so that FailNow's Goexit ends that " +
		"goroutine and not the test, and hands its result back over a channel; its parallel " +
		"tests are raced by make race-pipeline",
}

// TestRaceList_NamesEveryConcurrentPackage holds both race files to the tree.
// Outside the roots of raceRootsFile, a directory that concurrencyReasons flags
// is on the list or in raceExempt; under a root only a sync import or a
// goroutine counts, and the directory is on the list or in racePipelineOnly.
// No entry, deferral, exemption or root names what does not need it.
func TestRaceList_NamesEveryConcurrentPackage(t *testing.T) {
	t.Parallel()
	for _, v := range liveRaceViolations(t) {
		t.Error(v)
	}
}

// TestRaceList_TheRootsFileNamesEachPipelineTree holds that raceRootsFile names
// ir, pass and compilers, the guard against a dropped root. The tree half
// belongs to TestRaceList_NamesEveryConcurrentPackage, which a dropped root
// also reddens today, since every package under a root has a parallel test,
// which counts as a signal once the package lies under no root. This assertion
// holds even when that stops being true.
func TestRaceList_TheRootsFileNamesEachPipelineTree(t *testing.T) {
	t.Parallel()
	roots, err := readRaceList(irtest.RepoRoot(t), raceRootsFile, raceRootEntry)
	require.NoError(t, err)
	for _, want := range []string{"ir", "pass", "compilers"} {
		assert.Contains(t, roots, want, "%s names no tree for the pipeline's %s", raceRootsFile, want)
	}
}

// liveRaceViolations returns raceListViolations for the repository's own tree,
// with its two files and the real raceExempt and racePipelineOnly maps, which
// the planted tests do not read.
func liveRaceViolations(t *testing.T) []string {
	t.Helper()
	root := irtest.RepoRoot(t)
	roots, err := readRaceList(root, raceRootsFile, raceRootEntry)
	require.NoError(t, err)
	entries, err := readRaceList(root, raceListFile, raceEntry)
	require.NoError(t, err)
	rule := raceRule{roots: roots, entries: entries, exempt: raceExempt, pipelineOnly: racePipelineOnly}
	violations, err := raceListViolations(root, rule)
	require.NoError(t, err)
	return violations
}

// TestParseRaceList_ReadsTheFormatTheMakefileReads feeds the reader one file it
// accepts and one for each shape it refuses, for the list and the roots. The
// reader follows the format the Makefile's sed is written for, but no test runs
// the sed, so a change to the sed is checked with `make -n race` and
// `make -n race-pipeline`.
func TestParseRaceList_ReadsTheFormatTheMakefileReads(t *testing.T) {
	t.Parallel()
	src := "# a comment\n\n   \n  # an indented comment\n./a\n./b/c/...\n./d\n"
	got, err := parseRaceList(raceListFile, src, raceEntry)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b/c/...", "d"}, got)

	roots, err := parseRaceList(raceRootsFile, "# c\n./a/...\n./b/c/...\n", raceRootEntry)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b/c"}, roots, "a root is its directory, without the subtree suffix")

	tests := []struct {
		name string
		src  string
		want string
	}{
		{"no entries", "# only a comment\n\n", "lists no pattern"},
		{"empty file", "", "lists no pattern"},
		{"duplicate", "./a\n./b\n./a\n", "line 3: duplicate"},
		{"no ./ prefix", "# c\na\n", "line 2: \"a\" does not start with"},
		{"inner whitespace", "./a b\n", "line 1: \"./a b\" holds whitespace"},
		{"trailing comment", "./a#x\n", "line 1: \"./a#x\" holds whitespace or '#'"},
		{"indented entry", "  ./a\n", "line 1: \"  ./a\" holds whitespace"},
		{"the root", "./\n", "line 1: \"./\" does not name a directory"},
		{"outside the root", "./../a\n", "line 1: \"./../a\" does not name a directory"},
		{"the root as a dot", "./.\n", "line 1: \"./.\" does not name a directory"},
		{"a dot segment", "./a/../b\n", "line 1: \"./a/../b\" does not name a directory"},
		{"a wildcard inside", "./a/.../b\n", "line 1: \"./a/.../b\" does not name a directory"},
		{"the whole module", "./...\n", "line 1: \"./...\" does not name a directory"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseRaceList(raceListFile, tc.src, raceEntry)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}

	rootCases := []struct {
		name string
		src  string
		want string
	}{
		{"no roots", "# only a comment\n", raceRootsFile + " lists no pattern"},
		{"a package, not a subtree", "./a/...\n./b\n", raceRootsFile + " line 2: \"./b\" is not a subtree"},
		{"a duplicate root", "./a/...\n./a/...\n", raceRootsFile + " line 2: duplicate"},
		{"no ./ prefix", "a/...\n", raceRootsFile + " line 1: \"a/...\" does not start with"},
		{"the whole module", "./...\n", raceRootsFile + " line 1: \"./...\" does not name a directory"},
	}
	for _, tc := range rootCases {
		t.Run("roots "+tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseRaceList(raceRootsFile, tc.src, raceRootEntry)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestReadRaceList_FailsOnAMissingFile holds that a list or a roots file that
// cannot be read is an error, not an empty list that would flag every directory.
func TestReadRaceList_FailsOnAMissingFile(t *testing.T) {
	t.Parallel()
	_, err := readRaceList(t.TempDir(), raceListFile, raceEntry)
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = readRaceList(t.TempDir(), raceRootsFile, raceRootEntry)
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
	// Reported: a sibling whose name only starts with the entry's is not under it.
	writeGoStmt(t, filepath.Join(root, "nestedx", "w_test.go"), "nestedx")
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

	got, err := raceListViolations(root, raceRule{entries: entries, exempt: exempt})
	require.NoError(t, err)
	want := []string{
		`./calm/...: covers no package that needs the race run`,
		`./gone: covers no package that needs the race run`,
		`both: is on the race list and in raceExempt, which are exclusive`,
		`calm: is in raceExempt but needs no race run`,
		`nestedx: needs the race run (nestedx/w_test.go starts a goroutine) but is on no race list, in no ` +
			`raceExempt and in no racePipelineOnly`,
		`unlisted: needs the race run (unlisted/p.go imports "sync") but is on no race list, in no ` +
			`raceExempt and in no racePipelineOnly`,
		`vanished: is in raceExempt but needs no race run`,
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("report differs (-want +got):\n%s", d)
	}
}

// TestRaceListViolations_NamesEveryReasonAndEveryFile holds that a directory
// flagged by each signal reports each file with the reason it met, in path order.
func TestRaceListViolations_NamesEveryReasonAndEveryFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeNamedGoCall(t, filepath.Join(root, "d", "b_test.go"), "d")
	writeDocFile(t, filepath.Join(root, "d", "a.go"), "package d\n\nimport \"sync/atomic\"\n\nvar n atomic.Int64\n")
	writeGoStmt(t, filepath.Join(root, "d", "c.go"), "d")
	writeParallelTest(t, filepath.Join(root, "d", "e_test.go"), "d")

	got, err := raceListViolations(root, raceRule{entries: []string{"d"}})
	require.NoError(t, err)
	require.Empty(t, got, "a listed directory is not reported")

	got, err = raceListViolations(root, raceRule{})
	require.NoError(t, err)
	want := []string{`d: needs the race run (d/a.go imports "sync/atomic", d/b_test.go starts a goroutine, ` +
		`d/c.go starts a goroutine, d/e_test.go runs tests in parallel) but is on no race list, in no ` +
		`raceExempt and in no racePipelineOnly`}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("report differs (-want +got):\n%s", d)
	}
}

// TestRaceListViolations_PipelineRootsCountOnlyWhatMakeRaceReaches plants each
// shape the rule reports and each it leaves alone around one root, and expects
// exactly the report. Under the root a parallel test is no signal, a goroutine
// or a sync import is, and a directory showing one is listed or deferred to
// racePipelineOnly, never both and never exempt. Outside it a parallel test
// counts, and a deferral belongs in raceExempt.
func TestRaceListViolations_PipelineRootsCountOnlyWhatMakeRaceReaches(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Not reported: parallel tests alone, unlisted, under the root.
	writeParallelTest(t, filepath.Join(root, "pipe", "par", "p_test.go"), "par")
	// Reported: a goroutine, unlisted and not deferred.
	writeGoStmt(t, filepath.Join(root, "pipe", "gostmt", "w_test.go"), "gostmt")
	// Not reported: a sync import, deferred.
	writeDocFile(t, filepath.Join(root, "pipe", "syncd", "p.go"), "package p\n\nimport \"sync\"\n\nvar mu sync.Mutex\n")
	// Reported: deferred and listed.
	writeGoStmt(t, filepath.Join(root, "pipe", "both", "w_test.go"), "both")
	// Reported: deferred with parallel tests alone, which is no signal.
	writeParallelTest(t, filepath.Join(root, "pipe", "calm", "p_test.go"), "calm")
	// Reported: listed with parallel tests alone, so the entry covers nothing needed.
	writeParallelTest(t, filepath.Join(root, "pipe", "listedpar", "p_test.go"), "listedpar")
	// Reported: exempt under the root, whether it shows a signal or not.
	writeGoStmt(t, filepath.Join(root, "pipe", "skipped", "w_test.go"), "skipped")
	// Reported: outside the root, a sibling whose name only starts with the root's.
	writeParallelTest(t, filepath.Join(root, "pipex", "p_test.go"), "pipex")
	// Not reported: testdata.
	writeGoStmt(t, filepath.Join(root, "pipe", "testdata", "w.go"), "fixture")
	// Reported: outside the roots, t.Parallel() alone is a signal.
	writeParallelTest(t, filepath.Join(root, "tool", "p_test.go"), "tool")
	// Reported: outside the roots, a deferral belongs in raceExempt.
	writeGoStmt(t, filepath.Join(root, "outside", "w_test.go"), "outside")
	// Not reported: a production file's Parallel(), and a test file's Parallel(x).
	writeDocFile(t, filepath.Join(root, "prod", "p.go"), "package p\n\nfunc f(x interface{ Parallel() }) { x.Parallel() }\n")
	writeDocFile(t, filepath.Join(root, "arg", "p_test.go"), "package p\n\nfunc f(x interface{ Parallel(int) }) { x.Parallel(1) }\n")
	// Not reported: outside the roots, a test's no-argument call to another method.
	writeDocFile(t, filepath.Join(root, "helperonly", "p_test.go"),
		"package p\n\nimport \"testing\"\n\nfunc TestRuns(t *testing.T) {\n\tt.Helper()\n}\n")
	// Reported: a subtree entry under a root widens `make race` to packages it leaves to
	// `make race-pipeline`, though the directory it covers does show a signal.
	writeGoStmt(t, filepath.Join(root, "pipe", "wide", "w_test.go"), "wide")
	// Reported: an entry with no "/..." does not cover the directory below it.
	writeGoStmt(t, filepath.Join(root, "plain", "w_test.go"), "plain")
	writeGoStmt(t, filepath.Join(root, "plain", "child", "w_test.go"), "child")

	got, err := raceListViolations(root, raceRule{
		roots:        []string{"pipe", "gone"},
		entries:      []string{"pipe/both", "pipe/listedpar", "pipe/wide/...", "plain"},
		exempt:       map[string]string{"pipe/skipped": "r"},
		pipelineOnly: map[string]string{"pipe/syncd": "r", "pipe/both": "r", "pipe/calm": "r", "outside": "r"},
	})
	require.NoError(t, err)
	want := []string{
		`./gone/...: is a pipeline root covering no Go package`,
		`./pipe/listedpar: covers no package that needs the race run`,
		"./pipe/wide/...: is a subtree under a pipeline root, which `make race` names package by package",
		`outside: is in racePipelineOnly but lies under no pipeline root, so it belongs in raceExempt`,
		`pipe/both: is on the race list and in racePipelineOnly, which are exclusive`,
		`pipe/calm: is in racePipelineOnly but shows no sync import or goroutine`,
		`pipe/gostmt: needs the race run (pipe/gostmt/w_test.go starts a goroutine) but is on no race ` +
			`list, in no raceExempt and in no racePipelineOnly`,
		`pipe/skipped: is in raceExempt but lies under the pipeline root "pipe", whose packages cannot be exempt`,
		`pipex: needs the race run (pipex/p_test.go runs tests in parallel) but is on no race list, in no ` +
			`raceExempt and in no racePipelineOnly`,
		`plain/child: needs the race run (plain/child/w_test.go starts a goroutine) but is on no race list, in no ` +
			`raceExempt and in no racePipelineOnly`,
		`tool: needs the race run (tool/p_test.go runs tests in parallel) but is on no race list, in no ` +
			`raceExempt and in no racePipelineOnly`,
	}
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

	got, err := raceListViolations(root, raceRule{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, strings.HasPrefix(got[0], ".: needs the race run (w_test.go starts a goroutine)"), got[0])
}

// TestRaceListViolations_FailuresAreReturned holds that a file that does not
// parse ends the walk with an error rather than a clean report.
func TestRaceListViolations_FailuresAreReturned(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDocFile(t, filepath.Join(root, "p.go"), "package p\n\nfunc {")

	_, err := raceListViolations(root, raceRule{})
	require.Error(t, err)
}

// writeParallelTest plants a _test.go file whose test calls t.Parallel(), the
// signal a package's tests run concurrently.
func writeParallelTest(t *testing.T, file, pkg string) {
	t.Helper()
	writeDocFile(t, file, "package "+pkg+"\n\nimport \"testing\"\n\nfunc TestRuns(t *testing.T) {\n\tt.Parallel()\n}\n")
}

// readRaceList reads the file rel under root and returns its entries as
// parseRaceList does, each read by entry. It reads through os.ReadFile, which
// Go's test cache tracks, so editing the file re-runs the test that reads it; a
// child process reading it would leave a stale pass cached.
func readRaceList(root, rel string, entry func(string) (string, error)) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", rel, err)
	}
	return parseRaceList(rel, string(data), entry)
}

// parseRaceList returns what entry makes of each line of src, the text of file.
// It reads the format the Makefile's sed reads: a line whose first non-blank
// character is "#" is a comment, a blank line is skipped, and anything else is
// one pattern for entry to read. It refuses a line entry refuses, a duplicate
// and a file with no pattern, naming file and the line.
func parseRaceList(file, src string, entry func(string) (string, error)) ([]string, error) {
	var entries []string
	seen := map[string]int{}
	for i, line := range strings.Split(src, "\n") {
		n := i + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		e, err := entry(line)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", file, n, err)
		}
		if first, dup := seen[e]; dup {
			return nil, fmt.Errorf("%s line %d: duplicate of line %d", file, n, first)
		}
		seen[e] = n
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s lists no pattern, so the race run it feeds would add nothing", file)
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
	if dir == "." || !filepath.IsLocal(dir) || dir != path.Clean(dir) || strings.Contains(dir, "...") {
		return "", fmt.Errorf("%q does not name a directory below the root", line)
	}
	return rest, nil
}

// raceRootEntry returns the directory a line of the roots file names, without
// its "/...", or an error saying how the line is not a subtree pattern.
func raceRootEntry(line string) (string, error) {
	entry, err := raceEntry(line)
	if err != nil {
		return "", err
	}
	dir, subtree := strings.CutSuffix(entry, raceSubtree)
	if !subtree {
		return "", fmt.Errorf("%q is not a subtree pattern ending in %q", line, raceSubtree)
	}
	return dir, nil
}

// concurrencyReasons returns why the file rel, parsed as f, shows its
// directory running code on more than one goroutine. Three things count: a
// non-test file importing "sync" or "sync/atomic", any file, test or not, that
// startsGoroutine recognizes, and, when parallel is set, a test file that
// callsParallel. A test file importing "sync" alone does not. A directory
// under a pipeline root passes false, since `make race-pipeline` races it
// whatever its tests do.
func concurrencyReasons(rel string, f *ast.File, parallel bool) []string {
	var reasons []string
	isTest := strings.HasSuffix(rel, "_test.go")
	if name, ok := importsSync(f); ok && !isTest {
		reasons = append(reasons, fmt.Sprintf("%s imports %q", rel, name))
	}
	if startsGoroutine(f) {
		reasons = append(reasons, rel+" starts a goroutine")
	}
	if parallel && isTest && callsParallel(f) {
		reasons = append(reasons, rel+" runs tests in parallel")
	}
	return reasons
}

// callsParallel reports whether f calls a method named Parallel with no
// argument, whatever its receiver's type, as t.Parallel() does. Like
// isNamedGoCall it reads the syntax alone, so a Parallel method of another type
// counts too, which errs towards requiring the race run.
func callsParallel(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return !found
		}
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == "Parallel" && len(call.Args) == 0 {
			found = true
		}
		return !found
	})
	return found
}

// importsSync returns the first of "sync" and "sync/atomic" that f imports, and
// whether there is one.
func importsSync(f *ast.File) (string, bool) {
	for _, imp := range f.Imports {
		switch p := strings.Trim(imp.Path.Value, `"`); p {
		case "sync", "sync/atomic":
			return p, true
		default:
		}
	}
	return "", false
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

// pipelineRootOf returns the entry of roots that dir is or lies under, and
// whether there is one.
func pipelineRootOf(roots []string, dir string) (string, bool) {
	for _, r := range roots {
		if dir == r || strings.HasPrefix(dir, r+"/") {
			return r, true
		}
	}
	return "", false
}

// raceTree is what raceNeeds found under a root: the directories holding Go
// files, and for each that `make race` must cover, the reasons it must.
type raceTree struct {
	pkgs  map[string]bool
	needs map[string][]string
}

// raceNeeds returns the raceTree under root: for each directory, the reasons
// concurrencyReasons gives its files, sorted, counting a test's t.Parallel()
// only for a directory under none of roots. Directories are keyed by path
// relative to root, slash-separated, "." for root itself.
func raceNeeds(root string, roots []string) (raceTree, error) {
	tree := raceTree{pkgs: map[string]bool{}, needs: map[string][]string{}}
	err := walkGoFiles(root, func(rel string, _ *token.FileSet, f *ast.File) {
		dir := path.Dir(rel)
		tree.pkgs[dir] = true
		_, underRoot := pipelineRootOf(roots, dir)
		tree.needs[dir] = append(tree.needs[dir], concurrencyReasons(rel, f, !underRoot)...)
	})
	if err != nil {
		return raceTree{}, err
	}
	for dir, reasons := range tree.needs {
		if len(reasons) == 0 {
			delete(tree.needs, dir)
		}
		slices.Sort(reasons)
	}
	return tree, nil
}

// raceListViolations returns, sorted, one message for each way rule disagrees
// with the directories under root that need `make race` (raceNeeds): one that
// is on no list, in no raceExempt and in no racePipelineOnly; an entry that
// covers no such directory; a subtree entry under a root; a root covering no
// Go package; and each problem
// exemptionProblem and deferralProblem name. Paths are relative to root.
func raceListViolations(root string, rule raceRule) ([]string, error) {
	tree, err := raceNeeds(root, rule.roots)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, dir := range slices.Sorted(maps.Keys(tree.needs)) {
		_, isExempt := rule.exempt[dir]
		_, isDeferred := rule.pipelineOnly[dir]
		if !isExempt && !isDeferred && !raceListCovers(rule.entries, dir) {
			out = append(out, fmt.Sprintf(
				"%s: needs the race run (%s) but is on no race list, in no raceExempt and in no racePipelineOnly",
				dir, strings.Join(tree.needs[dir], ", ")))
		}
	}
	for _, e := range rule.entries {
		if !slices.ContainsFunc(slices.Collect(maps.Keys(tree.needs)), func(dir string) bool { return raceCovers(e, dir) }) {
			out = append(out, "./"+e+": covers no package that needs the race run")
		}
		if dir, subtree := strings.CutSuffix(e, raceSubtree); subtree {
			if _, under := pipelineRootOf(rule.roots, dir); under {
				out = append(out, "./"+e+": is a subtree under a pipeline root, which `make race` names package by package")
			}
		}
	}
	for _, r := range rule.roots {
		if !slices.ContainsFunc(slices.Collect(maps.Keys(tree.pkgs)), func(dir string) bool {
			_, under := pipelineRootOf([]string{r}, dir)
			return under
		}) {
			out = append(out, "./"+r+raceSubtree+": is a pipeline root covering no Go package")
		}
	}
	for _, dir := range slices.Sorted(maps.Keys(rule.exempt)) {
		out = append(out, exemptionProblem(rule, tree.needs, dir)...)
	}
	for _, dir := range slices.Sorted(maps.Keys(rule.pipelineOnly)) {
		out = append(out, deferralProblem(rule, tree.needs, dir)...)
	}
	slices.Sort(out)
	return out, nil
}

// raceListCovers reports whether any of entries covers dir.
func raceListCovers(entries []string, dir string) bool {
	return slices.ContainsFunc(entries, func(e string) bool { return raceCovers(e, dir) })
}

// exemptionProblem returns the message for the raceExempt entry dir, or nil
// when it is sound: dir lies under none of rule.roots, needs the race run
// (needs has it), and is not on the list.
func exemptionProblem(rule raceRule, needs map[string][]string, dir string) []string {
	root, underRoot := pipelineRootOf(rule.roots, dir)
	switch _, needed := needs[dir]; {
	case underRoot:
		return []string{dir + ": is in raceExempt but lies under the pipeline root " +
			strconv.Quote(root) + ", whose packages cannot be exempt"}
	case !needed:
		return []string{dir + ": is in raceExempt but needs no race run"}
	case raceListCovers(rule.entries, dir):
		return []string{dir + ": is on the race list and in raceExempt, which are exclusive"}
	default:
		return nil
	}
}

// deferralProblem returns the message for the racePipelineOnly entry dir, or
// nil when it is sound: dir lies under one of rule.roots, shows a signal
// `make race` counts there (needs has it), and is not on the list.
func deferralProblem(rule raceRule, needs map[string][]string, dir string) []string {
	_, underRoot := pipelineRootOf(rule.roots, dir)
	switch _, needed := needs[dir]; {
	case !underRoot:
		return []string{dir + ": is in racePipelineOnly but lies under no pipeline root, so it belongs in raceExempt"}
	case !needed:
		return []string{dir + ": is in racePipelineOnly but shows no sync import or goroutine"}
	case raceListCovers(rule.entries, dir):
		return []string{dir + ": is on the race list and in racePipelineOnly, which are exclusive"}
	default:
		return nil
	}
}
