package archtest_test

import (
	"fmt"
	"go/ast"
	"go/token"
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

// TestTestHelperViolations_PlantedShapesAreCaught plants each kind of function
// the rule reaches and each it leaves alone, in a package's test files.
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
		{"called, its parameter named otherwise", "func check(tb *testing.T) { tb.Fail() }\n" + caller, line},
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
// read. A method is left out: a call to one names no function a table could hold.
func testHelperViolations(root string) ([]string, int, error) {
	called := map[string]bool{}
	var pending []pendingHelper
	helpers := 0
	err := walkGoFiles(root, func(rel string, fset *token.FileSet, f *ast.File) {
		dir := path.Dir(rel)
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok {
					called[dir+"/"+id.Name] = true
				}
			}
			return true
		})
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			param, ok := testingParam(fn)
			if !ok || !asserts(fn.Body, param) {
				continue
			}
			helpers++
			if !startsWithHelper(fn.Body, param) {
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

// testingParam returns the name of fn's first parameter when its type is
// *testing.T or testing.TB, the two whose Helper reports a test's line.
func testingParam(fn *ast.FuncDecl) (string, bool) {
	params := fn.Type.Params.List
	if len(params) == 0 || len(params[0].Names) == 0 {
		return "", false
	}
	typ := params[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	sel, ok := typ.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "testing" || (sel.Sel.Name != "T" && sel.Sel.Name != "TB") {
		return "", false
	}
	return params[0].Names[0].Name, true
}

// asserts reports whether body fails its test by itself, through testify or a
// failing method of param. A function literal's body is left out: a failure
// there is placed by whatever runs it, and it holds its own Helper call.
func asserts(body *ast.BlockStmt, param string) bool {
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
		if x.Name == "assert" || x.Name == "require" ||
			(x.Name == param && slices.Contains(failingMethods, sel.Sel.Name)) {
			found = true
		}
		return true
	})
	return found
}

// startsWithHelper reports whether body's first statement is param.Helper().
func startsWithHelper(body *ast.BlockStmt, param string) bool {
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
	return ok && x.Name == param
}
