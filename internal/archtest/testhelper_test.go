// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir/irtest"
)

// failingMethods are the testing methods that fail a test from where they are
// called, which is what t.Helper moves to the caller.
var failingMethods = []string{"Error", "Errorf", "Fail", "FailNow", "Fatal", "Fatalf"}

// TestTestHelpers_StartWithHelper holds a function that asserts on a test's
// behalf to t.Helper, as CONTRIBUTING.md asks. thelper cannot state that: it
// reads a signature, and a table's case bodies, which are to report their own
// lines, share a helper's. What tells them apart is a direct call or a name
// another package can call.
func TestTestHelpers_StartWithHelper(t *testing.T) {
	t.Parallel()
	violations, helpers, err := testHelperViolations(irtest.RepoRoot(t))
	require.NoError(t, err)
	require.Positive(t, helpers, "the walk found no asserting helper, so it checked nothing")
	for _, v := range violations {
		t.Error(v)
	}
}

// TestTestHelperViolations_PlantedShapesAreCaught plants, in one test file,
// each kind of function taking testing's own types that the rule reaches and
// each it leaves alone. The tests below plant what turns on other files.
func TestTestHelperViolations_PlantedShapesAreCaught(t *testing.T) {
	t.Parallel()
	const head = "package p\n\nimport (\n\t\"log\"\n\t\"testing\"\n\n\t\"github.com/stretchr/testify/assert\"\n" +
		"\t\"github.com/stretchr/testify/require\"\n)\n\n"
	const caller = "\nfunc TestP(t *testing.T) { check(t) }\n"
	const line = 11 // of the planted function, after head
	tests := []struct {
		name string
		src  string
		want int // the line reported, 0 for none
	}{
		{"called, asserting through testify", "func check(t *testing.T) { assert.True(t, true) }\n" + caller, line},
		{"called, asserting through require", "func check(t *testing.T) { require.True(t, true) }\n" + caller, line},
		{"called, failing through t", "func check(t *testing.T) { t.Fatal(1) }\n" + caller, line},
		{"called, taking a testing.TB", "func check(t testing.TB) { t.Error(1) }\n" + caller, line},
		{"called, taking a *testing.B", "func check(b *testing.B) { b.Fatal(1) }\n\nfunc BenchmarkP(b *testing.B) { check(b) }\n", line},
		{"called, taking a *testing.F", "func check(f *testing.F) { f.Fatal(1) }\n\nfunc FuzzP(f *testing.F) { check(f) }\n", line},
		{"called, failing through its second testing parameter",
			"func check(a, t *testing.T) { t.Fatal(a.Name()) }\n\nfunc TestP(t *testing.T) { check(t, t) }\n", line},
		{"called through parentheses", "func check(t *testing.T) { t.Fatal(1) }\n\nfunc TestP(t *testing.T) { (check)(t) }\n", line},
		{"called through a parenthesized instantiation",
			"func check[V any](t *testing.T, v V) { t.Fatal(v) }\n\nfunc TestP(t *testing.T) { (check[int])(t, 1) }\n", line},
		{"called, its parameter named otherwise", "func check(tb *testing.T) { tb.Fail() }\n" + caller, line},
		{"called, its testing parameter second",
			"func check(name string, t *testing.T) { t.Fatal(name) }\n\nfunc TestP(t *testing.T) { check(\"x\", t) }\n", line},
		{"called through an instantiation",
			"func check[V any](t *testing.T, v V) { t.Fatal(v) }\n\nfunc TestP(t *testing.T) { check[int](t, 1) }\n", line},
		{"called through an instantiation of two", "func check[K, V any](t *testing.T, k K, v V) { t.Fatal(k, v) }\n\n" +
			"func TestP(t *testing.T) { check[int, int](t, 1, 1) }\n", line},
		{"t.Helper second", "func check(t *testing.T) {\n\tassert.True(t, true)\n\tt.Helper()\n}\n" + caller, line},
		{"another package can call it", "func Check(t *testing.T) { t.Fatal(1) }\n", line},
		{"another value's Helper first", "func check(t *testing.T) {\n\tx.Helper()\n\tt.Fatal(1)\n}\n" + caller, line},
		{"t.Helper first", "func check(t *testing.T) {\n\tt.Helper()\n\tt.Fatal(1)\n}\n" + caller, 0},
		{"a table's case", "func check(t *testing.T) { t.Fatal(1) }\n\nvar cases = []func(*testing.T){check}\n", 0},
		{"failing something else", "func check(t *testing.T) { log.Fatal(1) }\n" + caller, 0},
		{"another package's T", "func check(t *other.T) { t.Fatal(1) }\n" + caller, 0},
		{"asserting nothing", "func check(t *testing.T) { _ = t.Name() }\n" + caller, 0},
		{"asserting only in a literal", "func check(t *testing.T) func() { return func() { t.Fatal(1) } }\n" + caller, 0},
		{"a test", "func TestQ(t *testing.T) { t.Fatal(1) }\n", 0},
		{"a benchmark", "func BenchmarkQ(b *testing.B) { b.Fatal(1) }\n", 0},
		{"a fuzz target", "func FuzzQ(f *testing.F) { f.Fatal(1) }\n", 0},
		{"a test named only by its prefix", "func Test(t *testing.T) { t.Fatal(1) }\n", 0},
		{"a test whose prefix a non-letter follows", "func Test_q(t *testing.T) { t.Fatal(1) }\n", 0},
		{"a name that only starts like a test", "func Testdata(t *testing.T) { t.Fatal(1) }\n", line},
		{"a name that only starts like a benchmark", "func Benchmarked(t *testing.T) { t.Fatal(1) }\n", line},
		{"a name that only starts like a fuzz target", "func Fuzzy(t *testing.T) { t.Fatal(1) }\n", line},
		{"a second testing parameter's Helper first",
			"func check(a, t *testing.T) {\n\tt.Helper()\n\tt.Fatal(a.Name())\n}\n\nfunc TestP(t *testing.T) { check(t, t) }\n", 0},
		{"an unnamed testing parameter", "func check(*testing.T) {}\n\nfunc TestP(t *testing.T) { check(t) }\n", 0},
		{"no testing parameter", "func check(n int) { _ = n }\n\nfunc use() { check(1) }\n", 0},
		{"a method", "type s struct{}\n\nfunc (s) check(t *testing.T) { t.Fatal(1) }\n\nfunc use(t *testing.T) { s{}.check(t) }\n", 0},
		{"an exported method", "type s struct{}\n\nfunc (s) Check(t *testing.T) { t.Fatal(1) }\n", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeDocFile(t, filepath.Join(root, "a/p_test.go"), head+tc.src)

			got, _, err := testHelperViolations(root)
			require.NoError(t, err)
			var want []string
			if tc.want != 0 {
				want = []string{fmt.Sprintf("a/p_test.go:%d: an asserting helper starts with its Helper call", tc.want)}
			}
			assert.Empty(t, cmp.Diff(want, got))
		})
	}
}

// TestTestHelperViolations_ReadsTheNamesAFileImports plants files importing
// testing and testify under names of their own, and packages that only share
// a name with them, since what a call means is set by the file's imports.
func TestTestHelperViolations_ReadsTheNamesAFileImports(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		want int // the line reported, 0 for none
	}{
		{"testing under two names",
			"package p\n\nimport (\n\t\"testing\"\n\ttst \"testing\"\n)\n\nfunc check(t *testing.T) { t.Fatal(1) }\n\n" +
				"func TestP(t *tst.T) { check(t) }\n", 8},
		{"testing under another name",
			"package p\n\nimport tst \"testing\"\n\nfunc check(t *tst.T) { t.Fatal(1) }\n\n" +
				"func TestP(t *tst.T) { check(t) }\n", 5},
		{"testify under another name",
			"package p\n\nimport (\n\t\"testing\"\n\n\ta \"github.com/stretchr/testify/assert\"\n)\n\n" +
				"func check(t *testing.T) { a.True(t, true) }\n\nfunc TestP(t *testing.T) { check(t) }\n", 9},
		{"another package named assert",
			"package p\n\nimport (\n\t\"testing\"\n\n\tassert \"example.com/check\"\n)\n\n" +
				"func check(t *testing.T) { assert.True(t, true) }\n\nfunc TestP(t *testing.T) { check(t) }\n", 0},
		{"another package whose last element is testing",
			"package p\n\nimport \"example.com/testing\"\n\nfunc check(t *testing.T) { t.Fatal(1) }\n\n" +
				"func use(t *testing.T) { check(t) }\n", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeDocFile(t, filepath.Join(root, "a/p_test.go"), tc.src)

			got, _, err := testHelperViolations(root)
			require.NoError(t, err)
			var want []string
			if tc.want != 0 {
				want = []string{fmt.Sprintf("a/p_test.go:%d: an asserting helper starts with its Helper call", tc.want)}
			}
			assert.Empty(t, cmp.Diff(want, got))
		})
	}
}

// TestTestHelperViolations_ReadsTheModulesTestingInterfaces plants helpers
// that take an interface the module declares in place of a testing type, as
// openapitest.TB stands in for *testing.T, and a type parameter constrained by
// either. Each is planted beside an interface it must not be taken for.
func TestTestHelperViolations_ReadsTheModulesTestingInterfaces(t *testing.T) {
	t.Parallel()
	const tb = "package a\n\nimport \"testing\"\n\ntype TB interface {\n\tHelper()\n\tFatal(...any)\n}\n\n" +
		"type Embeds interface{ testing.TB }\n\ntype Fails interface{ Fatal(...any) }\n"
	const caller = "\nfunc TestP(t *testing.T) { check(t) }\n"
	const imported = "package b\n\nimport (\n\t\"testing\"\n\n\t%s\"%s\"\n)\n\nfunc check(t %s) { t.Fatal(1) }\n" + caller
	tests := []struct {
		name  string
		files map[string]string // by path under the root
		want  string            // the line reported, "" for none
	}{
		{"its own package's", map[string]string{"a/tb.go": tb,
			"a/p_test.go": "package a\n\nimport \"testing\"\n\nfunc check(t TB) { t.Fatal(1) }\n" + caller}, "a/p_test.go:5"},
		{"another package's", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", module+"/a", "a.TB")}, "b/p_test.go:9"},
		{"another package's, imported under another name", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "x ", module+"/a", "x.TB")}, "b/p_test.go:9"},
		{"the module root's", map[string]string{"tb.go": strings.Replace(tb, "package a", "package morphic", 1),
			"b/p_test.go": fmt.Sprintf(imported, "", module, "morphic.TB")}, "b/p_test.go:9"},
		{"one embedding testing.TB", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", module+"/a", "a.Embeds")}, "b/p_test.go:9"},
		{"a type parameter constrained by a testing type",
			map[string]string{"a/p_test.go": "package a\n\nimport \"testing\"\n\nfunc check[T testing.TB](t T) { t.Fatal(1) }\n" +
				caller}, "a/p_test.go:5"},
		{"a type parameter constrained by a pointer to one",
			map[string]string{"a/p_test.go": "package a\n\nimport \"testing\"\n\nfunc check[T *testing.T](t T) { t.Fatal(1) }\n" +
				caller}, "a/p_test.go:5"},
		{"a type parameter constrained by a module interface", map[string]string{"a/tb.go": tb,
			"a/p_test.go": "package a\n\nimport \"testing\"\n\nfunc check[U any, T TB](t T) { t.Fatal(1) }\n" + caller},
			"a/p_test.go:5"},
		{"an interface with no Helper", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", module+"/a", "a.Fails")}, ""},
		{"a type parameter constrained otherwise",
			map[string]string{"a/p_test.go": "package a\n\nimport \"testing\"\n\nfunc check[T interface{ Fatal(...any) }](t T) " +
				"{ t.Fatal(1) }\n" + caller}, ""},
		{"a name another directory declares", map[string]string{"a/tb.go": tb,
			"b/p_test.go": "package b\n\nimport \"testing\"\n\nfunc check(t TB) { t.Fatal(1) }\n" + caller}, ""},
		{"a package outside the module", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", "example.com/a", "a.TB")}, ""},
		{"a path outside the module that names a directory", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", "a", "a.TB")}, ""},
		{"another import's type of the same name", map[string]string{"a/tb.go": tb,
			"b/p_test.go": "package b\n\nimport (\n\t\"testing\"\n\n\t_ \"" + module + "/a\"\n\tc \"example.com/c\"\n)\n\n" +
				"func check(t c.TB) { t.Fatal(1) }\n" + caller}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for name, src := range tc.files {
				writeDocFile(t, filepath.Join(root, filepath.FromSlash(name)), src)
			}

			got, _, err := testHelperViolations(root)
			require.NoError(t, err)
			var want []string
			if tc.want != "" {
				want = []string{tc.want + ": an asserting helper starts with its Helper call"}
			}
			assert.Empty(t, cmp.Diff(want, got))
		})
	}
}

// TestTestHelperViolations_EntryPointsAreTestFilesOwn pins that a function
// named like a test, benchmark or fuzz target is skipped only in a test file:
// the go tool runs none from another, so there it is a helper like any other.
func TestTestHelperViolations_EntryPointsAreTestFilesOwn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDocFile(t, filepath.Join(root, "a/p.go"), "package a\n\nimport \"testing\"\n\n"+
		"func TestSpec(t *testing.T) { t.Fatal(1) }\n\nfunc BenchmarkSpec(b *testing.B) { b.Fatal(1) }\n\n"+
		"func FuzzSeeds(f *testing.F) { f.Fatal(1) }\n")

	got, helpers, err := testHelperViolations(root)
	require.NoError(t, err)
	want := []string{
		"a/p.go:5: an asserting helper starts with its Helper call",
		"a/p.go:7: an asserting helper starts with its Helper call",
		"a/p.go:9: an asserting helper starts with its Helper call",
	}
	assert.Empty(t, cmp.Diff(want, got))
	assert.Equal(t, 3, helpers)
}

// TestTestHelperViolations_CallsCountWithinAPackage pins that a call counts from
// any file of the helper's directory, and from none outside it: one helper of
// that name called elsewhere says nothing of a case body here.
func TestTestHelperViolations_CallsCountWithinAPackage(t *testing.T) {
	t.Parallel()
	const helper = "package p\n\nimport \"testing\"\n\nfunc check(t *testing.T) { t.Fatal(1) }\n"
	const caller = "package p\n\nimport \"testing\"\n\nfunc TestP(t *testing.T) { check(t) }\n"
	root := t.TempDir()
	writeDocFile(t, filepath.Join(root, "a/a_test.go"), helper)
	writeDocFile(t, filepath.Join(root, "a/b_test.go"), caller)
	writeDocFile(t, filepath.Join(root, "c/c_test.go"), helper)
	writeDocFile(t, filepath.Join(root, "d/d_test.go"), caller)

	got, helpers, err := testHelperViolations(root)
	require.NoError(t, err)
	want := []string{"a/a_test.go:5: an asserting helper starts with its Helper call"}
	assert.Empty(t, cmp.Diff(want, got))
	assert.Equal(t, 2, helpers)
}

// testHelperViolations returns a line for each function under root that breaks
// TestTestHelpers_StartWithHelper, sorted, and how many asserting functions it
// read. A method is left out, as a call to one is a selector this does not
// follow. So is a function run only through a value (f := check; f(t)), which
// reads as a table's case does.
func testHelperViolations(root string) ([]string, int, error) {
	ifaces, err := helperInterfaces(root)
	if err != nil {
		return nil, 0, err
	}
	called := map[string]bool{}
	var pending []pendingHelper
	helpers := 0
	err = walkGoFiles(root, func(rel string, fset *token.FileSet, f *ast.File) {
		dir := path.Dir(rel)
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := calledName(call.Fun); ok {
					called[dir+"/"+id] = true
				}
			}
			return true
		})
		ft := fileTypes{dir: dir, names: importNames(f), ifaces: ifaces}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || (strings.HasSuffix(rel, "_test.go") && isEntryPoint(fn.Name.Name)) {
				continue
			}
			params := ft.testingParams(fn)
			if len(params) == 0 || !asserts(fn.Body, params, ft.names) {
				continue
			}
			helpers++
			if !startsWithHelper(fn.Body, params) {
				pending = append(pending, pendingHelper{rel, dir, fn.Name.Name, fset.Position(fn.Pos()).Line})
			}
		}
	})
	if err != nil {
		return nil, 0, err
	}
	var violations []string
	for _, h := range pending {
		if called[h.dir+"/"+h.name] || ast.IsExported(h.name) {
			violations = append(violations,
				fmt.Sprintf("%s:%d: an asserting helper starts with its Helper call", h.rel, h.line))
		}
	}
	slices.Sort(violations)
	return violations, helpers, nil
}

// pendingHelper is a function that asserts and does not start with t.Helper,
// which breaks the rule if something calls it directly: the calls are not all
// read until the walk ends.
type pendingHelper struct {
	rel, dir, name string
	line           int
}

// assertPaths are the testify packages whose functions fail a test from where
// they are called.
var assertPaths = []string{"github.com/stretchr/testify/assert", "github.com/stretchr/testify/require"}

// importNames maps each import path f names to the names it is imported
// under: its alias, or else the path's last element, which is the package's
// name for testing, testify and the module's packages, the paths this reads.
// A local that shadows one of these names is still read as the import, as
// this reads syntax and not types; revive's import-shadowing refuses such a
// local.
func importNames(f *ast.File) map[string][]string {
	names := map[string][]string{}
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := path.Base(p)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		names[p] = append(names[p], name)
	}
	return names
}

// calledName returns the name a call's function is written as, through an
// instantiation such as check[int] and through parentheses.
func calledName(fun ast.Expr) (string, bool) {
	switch x := ast.Unparen(fun).(type) {
	case *ast.IndexExpr:
		fun = x.X
	case *ast.IndexListExpr:
		fun = x.X
	default:
	}
	id, ok := ast.Unparen(fun).(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

// testingKinds are the testing types a helper can take, each of which has a
// Helper that moves a failure's line to the caller.
var testingKinds = []string{"T", "B", "F", "TB"}

// entryPrefixes start the names of the functions the go tool runs itself,
// whose failures are their own lines.
var entryPrefixes = []string{"Test", "Benchmark", "Fuzz"}

// isEntryPoint reports whether name is one the go tool runs itself when a test
// file declares it: a prefix followed by nothing or by a rune that is not
// lowercase, as go help testfunc says. Benchmarked is a helper's name, not a
// benchmark's.
func isEntryPoint(name string) bool {
	return slices.ContainsFunc(entryPrefixes, func(p string) bool {
		rest, ok := strings.CutPrefix(name, p)
		r, _ := utf8.DecodeRuneInString(rest)
		return ok && (rest == "" || !unicode.IsLower(r))
	})
}

// helperInterfaces returns, keyed dir/name, each interface a file under root
// declares at top level whose own methods include Helper or that embeds a
// testing type: a helper can take one in place of *testing.T, as
// openapitest.TB stands in for it. One that has Helper only through a module
// interface it embeds is not read.
func helperInterfaces(root string) (map[string]bool, error) {
	out := map[string]bool{}
	err := walkGoFiles(root, func(rel string, _ *token.FileSet, f *ast.File) {
		ft := fileTypes{names: importNames(f)}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if it, ok := ts.Type.(*ast.InterfaceType); ok && ft.declaresHelper(it) {
					out[path.Dir(rel)+"/"+ts.Name.Name] = true
				}
			}
		}
	})
	return out, err
}

// fileTypes is what reading a type in one file needs: the file's directory
// under the root, what importNames read of it, and helperInterfaces' result.
// A directory stands for its package, as only code that builds is read.
type fileTypes struct {
	dir    string
	names  map[string][]string
	ifaces map[string]bool
}

// declaresHelper reports whether it names a Helper method or embeds a testing
// type.
func (ft fileTypes) declaresHelper(it *ast.InterfaceType) bool {
	return slices.ContainsFunc(it.Methods.List, func(m *ast.Field) bool {
		if len(m.Names) == 0 {
			return ft.isTesting(m.Type, nil)
		}
		return slices.ContainsFunc(m.Names, func(n *ast.Ident) bool { return n.Name == "Helper" })
	})
}

// testingParams returns the names of fn's parameters, in any position, whose
// type isTesting reads as a testing type, through fn's own type parameters. A
// variadic parameter is left out, as its elements are not read by name, and so
// is a constraint that is a union or an interface literal.
func (ft fileTypes) testingParams(fn *ast.FuncDecl) []string {
	var typeParams []string
	if fn.Type.TypeParams != nil {
		typeParams = ft.namesOf(fn.Type.TypeParams.List, nil)
	}
	return ft.namesOf(fn.Type.Params.List, typeParams)
}

// namesOf returns the names of fields whose type isTesting reads as a testing
// type.
func (ft fileTypes) namesOf(fields []*ast.Field, typeParams []string) []string {
	var out []string
	for _, field := range fields {
		if !ft.isTesting(field.Type, typeParams) {
			continue
		}
		for _, name := range field.Names {
			out = append(out, name.Name)
		}
	}
	return out
}

// isTesting reports whether typ, less any pointer, is one of testingKinds, an
// interface helperInterfaces found, or one of typeParams, whose constraints
// are such types. A module interface is written bare in its own directory and
// through an import elsewhere. A local alias of a testing type is not read.
func (ft fileTypes) isTesting(typ ast.Expr, typeParams []string) bool {
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	switch x := typ.(type) {
	case *ast.Ident:
		return ft.ifaces[ft.dir+"/"+x.Name] || slices.Contains(typeParams, x.Name)
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		if !ok {
			return false
		}
		if slices.Contains(ft.names["testing"], pkg.Name) && slices.Contains(testingKinds, x.Sel.Name) {
			return true
		}
		for p, names := range ft.names {
			if dir, ok := moduleDir(p); ok && slices.Contains(names, pkg.Name) && ft.ifaces[dir+"/"+x.Sel.Name] {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// moduleDir returns the directory under the repository root that holds the
// module package at import path p.
func moduleDir(p string) (string, bool) {
	if p == module {
		return ".", true
	}
	return strings.CutPrefix(p, module+"/")
}

// asserts reports whether body fails its test by itself, through testify or a
// failing method of one of params; names is what importNames read of the file.
// A function literal's body is left out: a failure there is placed by whatever
// runs it. A testing parameter copied into another local before it fails is
// not followed, as this reads syntax and not data flow.
func asserts(body *ast.BlockStmt, params []string, names map[string][]string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if isAssertPackage(names, x.Name) || (slices.Contains(params, x.Name) && slices.Contains(failingMethods, sel.Sel.Name)) {
			found = true
		}
		return true
	})
	return found
}

// isAssertPackage reports whether name is what a file calls one of assertPaths.
func isAssertPackage(names map[string][]string, name string) bool {
	return slices.ContainsFunc(assertPaths, func(p string) bool { return slices.Contains(names[p], name) })
}

// startsWithHelper reports whether body's first statement is the Helper call
// of one of params.
func startsWithHelper(body *ast.BlockStmt, params []string) bool {
	if len(body.List) == 0 {
		return false
	}
	stmt, ok := body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := stmt.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Helper" {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && slices.Contains(params, x.Name)
}
