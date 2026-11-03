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

// raceSubtree is the suffix that makes a race list entry cover a directory's
// whole subtree, as it does in a go test pattern.
const raceSubtree = "/..."

// pipelineRoots names the trees that hold the compile pipeline. Every Go
// package directory under one is covered by a race list entry and may not be in
// raceExempt, whatever the package does: their tests run the pipeline on
// parallel goroutines, and a race on a path that only those tests take must not
// go unreported. A new pipeline tree, such as emitters, joins this set.
var pipelineRoots = []string{"ir", "pass", "compilers"}

// raceExempt names the directories that run code concurrently and are
// deliberately off the race list, each with the reason. All of them are
// tooling outside the pipeline, and no directory under pipelineRoots may be
// here. A race on a path that only an exempt package's own tests take goes
// unreported.
var raceExempt = map[string]string{
	"cmd/morphic-census": "tooling outside the pipeline: its parallel tests call run on separate " +
		"goroutines, each compiling specs through the engine into buffers of its own",
	"cmd/morphic-covergate": "tooling outside the pipeline: its parallel tests call run and " +
		"tallyOf over coverage profiles they write to their own temporary files",
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
		"type-checks packages through go/packages, each on a fixture of its own",
	"internal/exectest": "test infrastructure outside the pipeline: its parallel tests build " +
		"commands with Command and read their environments, and one runs /bin/sh",
	"internal/harness": "tooling outside the pipeline: #1271 left it off, and its parallel tests " +
		"call harness.Check on parallel goroutines",
	"internal/jsonshape": "tooling outside the pipeline: its parallel tests call Describe and " +
		"Read over fixture packages that internal/decls loads",
	"internal/keydoc": "tooling outside the pipeline: its parallel tests parse Markdown and " +
		"comments held in memory, and walk temporary trees of their own",
	"internal/lintrules": "tooling outside the pipeline: its parallel tests run the analyzers " +
		"over planted packages that go/packages loads",
}

// TestRaceList_NamesEveryConcurrentPackage holds scripts/race-packages.txt to
// the directories that need the race run: every package under pipelineRoots,
// and elsewhere each that concurrencyReasons flags. Each is covered by an entry,
// or exempt in raceExempt with a reason unless it lies under a pipeline root;
// no entry or exemption names a directory that does not need it.
func TestRaceList_NamesEveryConcurrentPackage(t *testing.T) {
	t.Parallel()
	entries, err := readRaceList(filepath.Join(irtest.RepoRoot(t), filepath.FromSlash(raceListFile)))
	require.NoError(t, err)

	for _, v := range liveRaceViolations(t, entries, raceExempt) {
		t.Error(v)
	}
}

// TestRaceList_TheLiveTreeNeedsEachPipelineRoot holds the roots the live rule
// reads. Every package under one carries the pipeline reason, a signal or not,
// so with no entry and no exemption the report names each tree. The live rule
// alone cannot show a dropped root: the list's subtree entries already cover
// every package under the roots, so dropping one removes only a need the list
// already meets.
func TestRaceList_TheLiveTreeNeedsEachPipelineRoot(t *testing.T) {
	t.Parallel()
	report := strings.Join(liveRaceViolations(t, nil, nil), "\n")
	for _, r := range []string{"ir", "pass", "compilers"} {
		assert.Contains(t, report, "pipeline code under "+r, "the report names no package under %s", r)
	}
}

// liveRaceViolations returns raceListViolations for the repository's own tree
// with the real pipeline roots, which the planted tests do not read.
func liveRaceViolations(t *testing.T, entries []string, exempt map[string]string) []string {
	t.Helper()
	violations, err := raceListViolations(irtest.RepoRoot(t), pipelineRoots, entries, exempt)
	require.NoError(t, err)
	return violations
}

// TestParseRaceList_ReadsTheFormatTheMakefileReads feeds the reader one file it
// accepts and one for each shape it refuses. The reader follows the format the
// Makefile's sed is written for, but no test runs the sed, so a change to the
// sed is checked with `make -n race`.
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
		{"outside the root", "./../a\n", "line 1: \"./../a\" does not name a directory"},
		{"the root as a dot", "./.\n", "line 1: \"./.\" does not name a directory"},
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

	got, err := raceListViolations(root, nil, entries, exempt)
	require.NoError(t, err)
	want := []string{
		`./calm/...: covers no package that needs the race run`,
		`./gone: covers no package that needs the race run`,
		`both: is on the race list and in raceExempt, which are exclusive`,
		`calm: is in raceExempt but needs no race run`,
		`nestedx: needs the race run (nestedx/w_test.go starts a goroutine) but is neither on the race ` +
			`list nor in raceExempt`,
		`unlisted: needs the race run (unlisted/p.go imports "sync") but is neither on the race list ` +
			`nor in raceExempt`,
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

	got, err := raceListViolations(root, nil, []string{"d"}, nil)
	require.NoError(t, err)
	require.Empty(t, got, "a listed directory is not reported")

	got, err = raceListViolations(root, nil, nil, nil)
	require.NoError(t, err)
	want := []string{`d: needs the race run (d/a.go imports "sync/atomic", d/b_test.go starts a goroutine, ` +
		`d/c.go starts a goroutine, d/e_test.go runs tests in parallel) but is neither on the race list nor in raceExempt`}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("report differs (-want +got):\n%s", d)
	}
}

// TestRaceListViolations_PipelineRootsNeedNoSignal plants each shape the
// pipeline rule reports and each it leaves alone, and expects exactly the
// report: a package under a root needs the race run whatever it does, may not
// be exempt, and outside the roots a test file's t.Parallel() counts alone.
func TestRaceListViolations_PipelineRootsNeedNoSignal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	plain := "package p\n"
	// Not reported: the root itself, a covered child and a covered subtree.
	writeDocFile(t, filepath.Join(root, "pipe", "p.go"), plain)
	writeDocFile(t, filepath.Join(root, "pipe", "ok", "p.go"), plain)
	// Reported: no signal and no entry, which only the roots make a need.
	writeDocFile(t, filepath.Join(root, "pipe", "quiet", "p.go"), plain)
	// Reported: the entry names the parent without "/...", so not its child.
	writeDocFile(t, filepath.Join(root, "pipe", "parent", "p.go"), plain)
	writeDocFile(t, filepath.Join(root, "pipe", "parent", "child", "p.go"), plain)
	// Reported: a signal as well as the root, both named.
	writeParallelTest(t, filepath.Join(root, "pipe", "signalled", "p_test.go"), "signalled")
	// Reported: a covered directory that is also exempt.
	writeDocFile(t, filepath.Join(root, "pipe", "covered", "p.go"), plain)
	// Reported: an uncovered directory that is exempt, so the exemption does not stand in.
	writeDocFile(t, filepath.Join(root, "pipe", "skipped", "p.go"), plain)
	// Not reported: a sibling whose name only starts with the root's, and testdata.
	writeDocFile(t, filepath.Join(root, "pipex", "p.go"), plain)
	writeDocFile(t, filepath.Join(root, "pipe", "testdata", "p.go"), plain)
	// Reported: outside the roots, a test file calling t.Parallel() is the only signal.
	writeParallelTest(t, filepath.Join(root, "tool", "p_test.go"), "tool")
	// Not reported: a production file's Parallel(), and a test file's Parallel(x).
	writeDocFile(t, filepath.Join(root, "prod", "p.go"), "package p\n\nfunc f(x interface{ Parallel() }) { x.Parallel() }\n")
	writeDocFile(t, filepath.Join(root, "arg", "p_test.go"), "package p\n\nfunc f(x interface{ Parallel(int) }) { x.Parallel(1) }\n")

	entries := []string{"pipe", "pipe/ok", "pipe/parent", "pipe/covered/..."}
	exempt := map[string]string{"pipe/covered": "r", "pipe/skipped": "r", "pipe/vanished": "r"}

	got, err := raceListViolations(root, []string{"pipe"}, entries, exempt)
	require.NoError(t, err)
	const cannot = `, whose packages cannot be exempt`
	want := []string{
		`pipe/covered: is in raceExempt but lies under the pipeline root "pipe"` + cannot,
		`pipe/parent/child: needs the race run (pipeline code under pipe) but is neither on the race ` +
			`list nor in raceExempt`,
		`pipe/quiet: needs the race run (pipeline code under pipe) but is neither on the race list ` +
			`nor in raceExempt`,
		`pipe/signalled: needs the race run (pipe/signalled/p_test.go runs tests in parallel, pipeline ` +
			`code under pipe) but is neither on the race list nor in raceExempt`,
		`pipe/skipped: is in raceExempt but lies under the pipeline root "pipe"` + cannot,
		`pipe/vanished: is in raceExempt but lies under the pipeline root "pipe"` + cannot,
		`tool: needs the race run (tool/p_test.go runs tests in parallel) but is neither on the race ` +
			`list nor in raceExempt`,
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

	got, err := raceListViolations(root, nil, nil, nil)
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

	_, err := raceListViolations(root, nil, nil, nil)
	require.Error(t, err)
}

// writeParallelTest plants a _test.go file whose test calls t.Parallel(), the
// signal a package's tests run concurrently.
func writeParallelTest(t *testing.T, file, pkg string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	src := "package " + pkg + "\n\nimport \"testing\"\n\nfunc TestRuns(t *testing.T) {\n\tt.Parallel()\n}\n"
	require.NoError(t, os.WriteFile(file, []byte(src), 0o644))
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

// concurrencyReasons returns why the file rel, parsed as f, shows its
// directory running code on more than one goroutine. Three things count: a
// non-test file importing "sync" or "sync/atomic", any file, test or not, that
// startsGoroutine recognizes, and a test file that callsParallel. A test file
// importing "sync" alone does not.
func concurrencyReasons(rel string, f *ast.File) []string {
	var reasons []string
	isTest := strings.HasSuffix(rel, "_test.go")
	if name := importsSync(f); name != "" && !isTest {
		reasons = append(reasons, fmt.Sprintf("%s imports %q", rel, name))
	}
	if startsGoroutine(f) {
		reasons = append(reasons, rel+" starts a goroutine")
	}
	if isTest && callsParallel(f) {
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

// pipelineRootOf returns the entry of roots that dir is or lies under, or "".
func pipelineRootOf(roots []string, dir string) string {
	for _, r := range roots {
		if dir == r || strings.HasPrefix(dir, r+"/") {
			return r
		}
	}
	return ""
}

// raceNeeds returns, for each directory under root that the race run must
// cover, the reasons it does: those concurrencyReasons gives its files, and for
// a directory holding a Go file under one of roots, that it is pipeline code.
// Reasons are sorted. Directories are keyed by path relative to root,
// slash-separated, "." for root itself.
func raceNeeds(root string, roots []string) (map[string][]string, error) {
	needs := map[string][]string{}
	err := walkGoFiles(root, func(rel string, _ *token.FileSet, f *ast.File) {
		dir := path.Dir(rel)
		needs[dir] = append(needs[dir], concurrencyReasons(rel, f)...)
		if r := pipelineRootOf(roots, dir); r != "" && !slices.Contains(needs[dir], "pipeline code under "+r) {
			needs[dir] = append(needs[dir], "pipeline code under "+r)
		}
	})
	if err != nil {
		return nil, err
	}
	for dir, reasons := range needs {
		if len(reasons) == 0 {
			delete(needs, dir)
		}
		slices.Sort(reasons)
	}
	return needs, nil
}

// raceListViolations returns, sorted, one message for each way the race list
// and exempt (directory to reason) disagree with the directories under root
// that need the race run (raceNeeds): one that is neither covered by an entry
// nor exempt; an entry that covers no such directory, a directory the entry
// does not name included; an exemption under one of roots, which no pipeline
// package may have; an exemption for a directory that needs no race run; and
// one that is both covered and exempt. Paths are relative to root.
func raceListViolations(root string, roots, entries []string, exempt map[string]string) ([]string, error) {
	needs, err := raceNeeds(root, roots)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, dir := range slices.Sorted(maps.Keys(needs)) {
		if _, isExempt := exempt[dir]; !isExempt && !raceListCovers(entries, dir) {
			out = append(out, fmt.Sprintf(
				"%s: needs the race run (%s) but is neither on the race list nor in raceExempt",
				dir, strings.Join(needs[dir], ", ")))
		}
	}
	for _, e := range entries {
		if !slices.ContainsFunc(slices.Collect(maps.Keys(needs)), func(dir string) bool { return raceCovers(e, dir) }) {
			out = append(out, "./"+e+": covers no package that needs the race run")
		}
	}
	for _, dir := range slices.Sorted(maps.Keys(exempt)) {
		out = append(out, exemptionProblem(roots, needs, dir, raceListCovers(entries, dir))...)
	}
	slices.Sort(out)
	return out, nil
}

// raceListCovers reports whether any of entries covers dir.
func raceListCovers(entries []string, dir string) bool {
	return slices.ContainsFunc(entries, func(e string) bool { return raceCovers(e, dir) })
}

// exemptionProblem returns the message for the raceExempt entry dir, or nil
// when it is sound: dir lies under none of roots, needs the race run (needs
// has it), and is not covered.
func exemptionProblem(roots []string, needs map[string][]string, dir string, covered bool) []string {
	switch _, needed := needs[dir]; {
	case pipelineRootOf(roots, dir) != "":
		return []string{dir + ": is in raceExempt but lies under the pipeline root " +
			strconv.Quote(pipelineRootOf(roots, dir)) + ", whose packages cannot be exempt"}
	case !needed:
		return []string{dir + ": is in raceExempt but needs no race run"}
	case covered:
		return []string{dir + ": is on the race list and in raceExempt, which are exclusive"}
	default:
		return nil
	}
}
