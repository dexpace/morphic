package archtest_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// leakcheckImport is the import path a test file must call Main through for
// goroutineLeakCheckViolations to credit its directory with the check.
const leakcheckImport = module + "/internal/leakcheck"

// TestGoroutineLeak_EveryGoStatementHasALeakCheck requires every directory
// whose .go files — production and test alike — start a goroutine to also
// have a _test.go file calling leakcheck.Main. A goroutine start is either an
// ast.GoStmt or a single-argument call to a method named Go, which is what
// sync.WaitGroup.Go and errgroup.Group.Go both are — engine's own concurrency
// test starts its workers exactly that way, with no go statement anywhere in
// the package. It is the guard that stops a fifth package from starting a
// goroutine in its tests without the check GitHub #514 added for the first
// four.
func TestGoroutineLeak_EveryGoStatementHasALeakCheck(t *testing.T) {
	t.Parallel()
	violations, err := goroutineLeakCheckViolations(repoRoot(t))
	require.NoError(t, err)
	for _, v := range violations {
		t.Error(v)
	}
}

// TestGoroutineLeakCheckViolations_PlantedGoStatementIsCaught proves the
// detector both ways, for each of the two shapes it recognizes: a directory
// with a bare go statement and no call is reported, and one where a _test.go
// file also calls leakcheck.Main is not; the same pair holds for a
// single-argument ".Go(...)" call, the shape a go statement's ast.GoStmt does
// not cover.
func TestGoroutineLeakCheckViolations_PlantedGoStatementIsCaught(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeGoStmt(t, filepath.Join(root, "unguarded", "worker_test.go"), "unguarded")
	writeGoStmt(t, filepath.Join(root, "guarded", "worker_test.go"), "guarded")
	writeLeakcheckMain(t, filepath.Join(root, "guarded", "main_test.go"), "guarded")
	writeNamedGoCall(t, filepath.Join(root, "unguarded_wg", "worker_test.go"), "unguarded_wg")
	writeNamedGoCall(t, filepath.Join(root, "guarded_wg", "worker_test.go"), "guarded_wg")
	writeLeakcheckMain(t, filepath.Join(root, "guarded_wg", "main_test.go"), "guarded_wg")

	violations, err := goroutineLeakCheckViolations(root)
	require.NoError(t, err)
	require.Len(t, violations, 2, "exactly the two unguarded directories are reported: %v", violations)
	assert.Contains(t, strings.Join(violations, "\n"), filepath.Join("unguarded", "worker_test.go"))
	assert.Contains(t, strings.Join(violations, "\n"), filepath.Join("unguarded_wg", "worker_test.go"))
}

// writeGoStmt plants a _test.go file that starts a goroutine, blocked forever
// on a channel nothing else holds — the shape the detector must recognize
// regardless of which package declares it.
func writeGoStmt(t *testing.T, file, pkg string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	src := "package " + pkg + "\n\nfunc leaks() {\n\tblock := make(chan struct{})\n\tgo func() {\n\t\t<-block\n\t}()\n}\n"
	require.NoError(t, os.WriteFile(file, []byte(src), 0o644))
}

// writeNamedGoCall plants a _test.go file that starts a goroutine through a
// single-argument ".Go(...)" call rather than a go statement — the shape
// sync.WaitGroup.Go and errgroup.Group.Go both are, and the one engine's own
// concurrency test uses.
func writeNamedGoCall(t *testing.T, file, pkg string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	src := "package " + pkg + "\n\nimport \"sync\"\n\nfunc leaks() {\n\tvar wg sync.WaitGroup\n\twg.Go(func() {})\n}\n"
	require.NoError(t, os.WriteFile(file, []byte(src), 0o644))
}

// writeLeakcheckMain plants a TestMain calling leakcheck.Main: the shape that
// satisfies the guard for a directory writeGoStmt also names.
func writeLeakcheckMain(t *testing.T, file, pkg string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	src := fmt.Sprintf("package %s\n\nimport (\n\t\"testing\"\n\n\t\"%s\"\n)\n\n"+
		"func TestMain(m *testing.M) { leakcheck.Main(m) }\n", pkg, leakcheckImport)
	require.NoError(t, os.WriteFile(file, []byte(src), 0o644))
}

// dirGoroutineState is what one directory's walk has found so far: whether any
// of its .go files starts a goroutine, which files those are, and whether some
// _test.go file already calls leakcheck.Main.
type dirGoroutineState struct {
	startsGoroutine bool
	hasCall         bool
	goroutineFiles  []string
}

// goroutineLeakCheckViolations walks root for every directory whose .go files
// — production and test alike — start a goroutine, and returns one message
// per such directory that has no _test.go file calling leakcheck.Main.
//
// Both kinds of file are read because every instance in this module today
// starts its goroutine from a _test.go file, and a production file is read the
// same way on the chance that changes.
func goroutineLeakCheckViolations(root string) ([]string, error) {
	dirs := map[string]*dirGoroutineState{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && ignoredByGoTool(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".go" {
			return nil
		}
		return recordGoroutineState(dirs, p)
	})
	if err != nil {
		return nil, err
	}
	return unguardedDirs(dirs), nil
}

// recordGoroutineState parses one file and folds what it finds into its
// directory's entry in dirs, creating that entry on first sight.
func recordGoroutineState(dirs map[string]*dirGoroutineState, p string) error {
	f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	state := dirs[dir]
	if state == nil {
		state = &dirGoroutineState{}
		dirs[dir] = state
	}
	if startsGoroutine(f) {
		state.startsGoroutine = true
		state.goroutineFiles = append(state.goroutineFiles, p)
	}
	if strings.HasSuffix(p, "_test.go") && callsLeakcheckMain(f) {
		state.hasCall = true
	}
	return nil
}

// unguardedDirs returns one message per directory in dirs that started a
// goroutine with no call recorded against it, sorted for a stable report.
func unguardedDirs(dirs map[string]*dirGoroutineState) []string {
	names := make([]string, 0, len(dirs))
	for dir := range dirs {
		names = append(names, dir)
	}
	sort.Strings(names)

	var violations []string
	for _, dir := range names {
		state := dirs[dir]
		if !state.startsGoroutine || state.hasCall {
			continue
		}
		sort.Strings(state.goroutineFiles)
		violations = append(violations, fmt.Sprintf(
			"%s: starts a goroutine (%s) with no _test.go file calling leakcheck.Main",
			dir, strings.Join(state.goroutineFiles, ", ")))
	}
	return violations
}

// startsGoroutine reports whether f contains a go statement, or a call shaped
// like sync.WaitGroup.Go / errgroup.Group.Go: a single-argument method call
// named Go. Both start a real goroutine, but only the first is an ast.GoStmt —
// engine's own concurrency test starts its workers through the second shape,
// with no go statement anywhere in the package.
//
// The second check is syntactic, not type-checked: it matches any
// single-argument ".Go(...)" call, whatever type the receiver has, rather than
// resolving it to confirm the receiver really is a sync.WaitGroup or an
// errgroup.Group. A false positive costs one harmless extra TestMain; a miss
// is exactly what this guard exists to prevent, so the syntactic match is
// deliberately the more permissive of the two mistakes.
func startsGoroutine(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.GoStmt:
			found = true
			return false
		case *ast.CallExpr:
			if isNamedGoCall(v) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// isNamedGoCall reports whether call is a single-argument method call named
// Go — the shape sync.WaitGroup.Go and errgroup.Group.Go both share, and the
// only trace a call like it leaves in the AST: nothing here names a receiver
// type, so any single-arg ".Go(...)" matches, on purpose.
func isNamedGoCall(call *ast.CallExpr) bool {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	return isSel && sel.Sel.Name == "Go" && len(call.Args) == 1
}

// callsLeakcheckMain reports whether f imports leakcheckImport and calls its
// Main function through the name that import binds — the package's own name
// by default, or an explicit alias.
func callsLeakcheckMain(f *ast.File) bool {
	local := importedAs(f, leakcheckImport)
	if local == "" {
		return false
	}
	called := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel {
			return true
		}
		if id, isIdent := sel.X.(*ast.Ident); isIdent && id.Name == local && sel.Sel.Name == "Main" {
			called = true
			return false
		}
		return true
	})
	return called
}

// importedAs returns the local name f's import of path binds — the alias when
// one is written, otherwise "leakcheck", the only package this checks for —
// or "" when f does not import it at all.
func importedAs(f *ast.File, path string) string {
	for _, imp := range f.Imports {
		if strings.Trim(imp.Path.Value, `"`) != path {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "leakcheck"
	}
	return ""
}
