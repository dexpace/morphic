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

// TestTestHelperViolations_PlantedShapesAreCaught plants functions the rule
// reaches and ones it leaves alone, each in one file that imports testing and
// testify under their own names. The tests below plant what turns on an
// import's name, on a declared type or a type parameter, or on which file or
// package holds a function.
func TestTestHelperViolations_PlantedShapesAreCaught(t *testing.T) {
	t.Parallel()
	const head = "package p\n\nimport (\n\t\"log\"\n\t\"testing\"\n\n\t\"github.com/stretchr/testify/assert\"\n" +
		"\t\"github.com/stretchr/testify/require\"\n)\n\n"
	const caller = "\nfunc TestP(t *testing.T) { check(t) }\n"
	const callerOfTwo = "\nfunc TestP(t *testing.T) { check(t, t) }\n"
	const callerByPointer = "\nfunc TestP(t *testing.T) {\n\tvar x testing.TB = t\n\tcheck(&x)\n}\n"
	const line = 11 // of the planted function, after head
	tests := []struct {
		name string
		src  string
		want int // the line reported, 0 for none
	}{
		{"called, asserting through testify", "func check(t *testing.T) { assert.True(t, true) }\n" + caller, line},
		{"called, asserting through a parenthesized testify call", "func check(t *testing.T) { (assert.True)(t, true) }\n" + caller, line},
		{"called, asserting through require", "func check(t *testing.T) { require.True(t, true) }\n" + caller, line},
		{"called, failing through t", "func check(t *testing.T) { t.Fatal(1) }\n" + caller, line},
		{"called, taking a testing.TB", "func check(t testing.TB) { t.Error(1) }\n" + caller, line},
		{"called, taking a *testing.B", "func check(b *testing.B) { b.Fatal(1) }\n\nfunc BenchmarkP(b *testing.B) { check(b) }\n", line},
		{"called, taking a *testing.F", "func check(f *testing.F) { f.Fatal(1) }\n\nfunc FuzzP(f *testing.F) { check(f) }\n", line},
		{"called, failing through its second testing parameter",
			"func check(a, t *testing.T) { t.Fatal(a.Name()) }\n" + callerOfTwo, line},
		{"called, failing through its second testing field",
			"func check(b *testing.B, t *testing.T) { t.Fatal(b.N) }\n\nfunc TestP(t *testing.T) { check(nil, t) }\n", line},
		{"called, failing through a parameter whose Helper it does not call",
			"func check(a, t *testing.T) {\n\tt.Helper()\n\ta.Fatal(t.Name())\n}\n" + callerOfTwo, line},
		{"called, failing through a field whose Helper it does not call",
			"func check(t *testing.T, b *testing.B) {\n\tb.Helper()\n\tt.Fatal(b.N)\n}\n\nfunc TestP(t *testing.T) { check(t, nil) }\n", line},
		{"called, asserting through testify on a parameter whose Helper it does not call",
			"func check(a, t *testing.T) {\n\ta.Helper()\n\tassert.True(t, a != nil)\n}\n" + callerOfTwo, line},
		{"called, asserting through testify on no parameter",
			"func check(a, t *testing.T) {\n\tt.Helper()\n\tassert.True(nil, a != nil)\n}\n" + callerOfTwo, line},
		{"called, failing through two parameters, one helped",
			"func check(a, t *testing.T) {\n\ta.Helper()\n\ta.Fatal(1)\n\tt.Fatal(1)\n}\n" + callerOfTwo, line},
		{"called through parentheses", "func check(t *testing.T) { t.Fatal(1) }\n\nfunc TestP(t *testing.T) { (check)(t) }\n", line},
		{"called through a parenthesized instantiation",
			"func check[V any](t *testing.T, v V) { t.Fatal(v) }\n\nfunc TestP(t *testing.T) { (check[int])(t, 1) }\n", line},
		{"called, taking a pointer to a parenthesized testing type", "func check(t *(testing.T)) { t.Fatal(1) }\n" + caller, line},
		{"called, failing through a parenthesized parameter", "func check(t *testing.T) { (t).Fatal(1) }\n" + caller, line},
		{"called, failing through a parenthesized method", "func check(t *testing.T) { (t.Fatal)(1) }\n" + caller, line},
		{"called, failing through a dereferenced parameter", "func check(t *testing.T) { (*t).Fatal(1) }\n" + caller, line},
		{"called, failing through a parenthesized dereferenced parameter", "func check(t *testing.T) { (*(t)).Fatal(1) }\n" + caller,
			line},
		{"called, failing through a pointer to an interface",
			"func check(t *testing.TB) { (*t).Fatal(1) }\n" + callerByPointer, line},
		{"called, failing through a method value", "func check(t *testing.T) {\n\tfail := t.Fatal\n\tfail(1)\n}\n" + caller, line},
		{"called, taking an interface literal", "func check(t interface {\n\tHelper()\n\tFatal(...any)\n}) {\n\tt.Fatal(1)\n}\n" + caller,
			line},
		{"called, its parameter named otherwise", "func check(tb *testing.T) { tb.Fail() }\n" + caller, line},
		{"called, its testing parameter second",
			"func check(name string, t *testing.T) { t.Fatal(name) }\n\nfunc TestP(t *testing.T) { check(t.Name(), t) }\n", line},
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
		{"taking another testing type",
			"func check(pb *testing.PB) { assert.True(nil, pb.Next()) }\n\nfunc use(pb *testing.PB) { check(pb) }\n", 0},
		{"taking an interface literal with no Helper", "func check(t interface{ Fatal(...any) }) { t.Fatal(1) }\n" + caller, 0},
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
		{"a parenthesized parameter's Helper first", "func check(t *testing.T) {\n\t(t).Helper()\n\tt.Fatal(1)\n}\n" + caller, 0},
		{"a parenthesized Helper first", "func check(t *testing.T) {\n\t(t.Helper)()\n\tt.Fatal(1)\n}\n" + caller, 0},
		{"a Helper call parenthesized whole", "func check(t *testing.T) {\n\t(t.Helper())\n\tt.Fatal(1)\n}\n" + caller, 0},
		{"a dereferenced parameter's Helper first",
			"func check(t *testing.TB) {\n\t(*t).Helper()\n\tassert.True(*t, false)\n}\n" + callerByPointer, 0},
		{"a second testing parameter's Helper first",
			"func check(a, t *testing.T) {\n\tt.Helper()\n\tt.Fatal(a.Name())\n}\n" + callerOfTwo, 0},
		{"each failing parameter's Helper first",
			"func check(a, t *testing.T) {\n\ta.Helper()\n\tt.Helper()\n\ta.Fatal(1)\n\tt.Fatal(1)\n}\n" + callerOfTwo, 0},
		{"the Helper first of the parameter testify fails through",
			"func check(a, t *testing.T) {\n\tt.Helper()\n\tassert.True(t, a != nil)\n}\n" + callerOfTwo, 0},
		{"the Helper first of the parameter a parenthesized testify call fails through",
			"func check(a, t *testing.T) {\n\tt.Helper()\n\t(assert.True)(t, a != nil)\n}\n" + callerOfTwo, 0},
		{"the Helper first of the parameter testify fails through, dereferenced",
			"func check(a, t *testing.TB) {\n\t(*t).Helper()\n\tassert.True(*t, a != nil)\n}\n" +
				"\nfunc TestP(t *testing.T) {\n\tvar x testing.TB = t\n\tcheck(&x, &x)\n}\n", 0},
		{"the first parameter's Helper first, testify on a copy",
			"func check(a, t *testing.T) {\n\ta.Helper()\n\ttb := a\n\tassert.True(tb, t != nil)\n}\n" + callerOfTwo, 0},
		{"the first parameter's Helper first, testify on nil",
			"func check(a, t *testing.T) {\n\ta.Helper()\n\tassert.True(nil, t != nil)\n}\n" + callerOfTwo, 0},
		{"the first parameter's Helper first, testify on a CollectT",
			"func check(a, t *testing.T, c *assert.CollectT) {\n\ta.Helper()\n\tassert.True(c, t != nil)\n}\n" +
				"\nfunc TestP(t *testing.T) { check(t, t, nil) }\n", 0},
		{"naming a testify type", "func check(t *testing.T) assert.TestingT {\n\tvar x assert.TestingT = t\n\treturn x\n}\n" + caller, 0},
		{"returning a testify function uncalled",
			"func check(t *testing.T) assert.BoolAssertionFunc {\n\t_ = t.Name()\n\treturn assert.True\n}\n" + caller, 0},
		{"a testify call with no argument", "func check(t *testing.T) {\n\t_ = t.Name()\n\t_ = assert.CallerInfo()\n}\n" + caller, 0},
		{"an unnamed testing parameter", "func check(*testing.T) {}\n\nfunc TestP(t *testing.T) { check(t) }\n", 0},
		{"a blank testing parameter", "func check(_ *testing.T, n int) { assert.True(nil, n > 0) }\n\nfunc use() { check(nil, 1) }\n", 0},
		{"no testing parameter", "func check(n int) { assert.True(nil, n > 0) }\n\nfunc use() { check(1) }\n", 0},
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

// importsTesting follows a package clause in a file that imports testing
// alone, so what follows it starts on the file's fifth line.
const importsTesting = `

import "testing"

`

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
		{"testing under two names", `package p

import (
	"testing"
	tst "testing"
)

func check(t *testing.T) { t.Fatal(1) }

func TestP(t *tst.T) { check(t) }
`, 8},
		{"testing under another name", `package p

import tst "testing"

func check(t *tst.T) { t.Fatal(1) }

func TestP(t *tst.T) { check(t) }
`, 5},
		{"testify under another name", `package p

import (
	"testing"

	a "github.com/stretchr/testify/assert"
)

func check(t *testing.T) { a.True(t, true) }

func TestP(t *testing.T) { check(t) }
`, 9},
		{"another package named assert", `package p

import (
	"testing"

	assert "example.com/check"
)

func check(t *testing.T) { assert.True(t, true) }

func TestP(t *testing.T) { check(t) }
`, 0},
		{"another package whose last element is testing", `package p

import "example.com/testing"

func check(t *testing.T) { t.Fatal(1) }

func use(t *testing.T) { check(t) }
`, 0},
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
			assert.Empty(t, cmp.Diff(want, got), "mismatch (-want +got)")
		})
	}
}

// TestTestHelperViolations_ReadsTheModulesTestingInterfaces plants helpers
// that take a type the module declares in place of a testing type, as
// openapitest.TB stands in for *testing.T, or a type parameter constrained by
// either. Each that takes a module type is planted beside one it must not be
// taken for, and a name is read in the scope that declares it: a type
// parameter's, or a package's beside its external test package.
func TestTestHelperViolations_ReadsTheModulesTestingInterfaces(t *testing.T) {
	t.Parallel()
	const tb = `package a

import "testing"

type TB interface {
	Helper()
	Fatal(...any)
}

type Embeds interface{ testing.TB }

type EmbedsParenthesized interface{ (testing.TB) }

type Fails interface{ Fatal(...any) }

type Of[X any] interface {
	Helper()
	Fatal(...any)
}

type OfTwo[X, Y any] interface {
	Helper()
	Fatal(...any)
}

type (
	n       int
	Grouped interface {
		Helper()
		Fatal(...any)
	}
)

type Alias = testing.TB

type Defined testing.T

type AliasParenthesized = (*testing.T)
`
	const caller = "\nfunc TestP(t *testing.T) { check(t) }\n"
	const imported = `package b

import (
	"testing"

	%s"%s"
)

func check(t %s) { t.Fatal(1) }
` + caller
	const viaTestify = `package a

import "github.com/stretchr/testify/assert"

`
	const pointee = "\n\ntype f struct{}\n\nfunc (f) Fatal(...any) {}\n\n"
	tests := []struct {
		name  string
		files map[string]string // by path under the root
		want  string            // the line reported, "" for none
	}{
		{"its own package's", map[string]string{"a/tb.go": tb,
			"a/p_test.go": "package a" + importsTesting + "func check(t TB) { t.Fatal(1) }\n" + caller}, "a/p_test.go:5"},
		{"another package's", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", module+"/a", "a.TB")}, "b/p_test.go:9"},
		{"another package's, imported under another name", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "x ", module+"/a", "x.TB")}, "b/p_test.go:9"},
		{"the module root's", map[string]string{"tb.go": strings.Replace(tb, "package a", "package morphic", 1),
			"b/p_test.go": fmt.Sprintf(imported, "", module, "morphic.TB")}, "b/p_test.go:9"},
		{"one embedding testing.TB", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", module+"/a", "a.Embeds")}, "b/p_test.go:9"},
		{"one embedding a parenthesized testing type", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", module+"/a", "a.EmbedsParenthesized")}, "b/p_test.go:9"},
		{"one declared in a group, past its first type", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", module+"/a", "a.Grouped")}, "b/p_test.go:9"},
		{"an alias of a testing type", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", module+"/a", "a.Alias")}, "b/p_test.go:9"},
		{"an alias of a parenthesized pointer to a testing type", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", module+"/a", "a.AliasParenthesized")}, "b/p_test.go:9"},
		{"a definition of a testing type", map[string]string{"a/tb.go": tb,
			"b/p_test.go": strings.Replace(fmt.Sprintf(imported, "", module+"/a", "*a.Defined"),
				"check(t)", "check((*a.Defined)(t))", 1)}, "b/p_test.go:9"},
		{"its own package's, instantiated", map[string]string{"a/tb.go": tb,
			"a/p_test.go": "package a" + importsTesting + "func check(t Of[int]) { t.Fatal(1) }\n" + caller}, "a/p_test.go:5"},
		{"another package's, instantiated with two", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", module+"/a", "a.OfTwo[int, int]")}, "b/p_test.go:9"},
		{"an external test package's own", map[string]string{"a/tb.go": tb,
			"a/p_test.go": "package a_test" + importsTesting + "type Own interface {\n\tHelper()\n\tFatal(...any)\n}\n\n" +
				"func check(t Own) { t.Fatal(1) }\n" + caller}, "a/p_test.go:10"},
		{"a type parameter constrained by a testing type", map[string]string{
			"a/p_test.go": "package a" + importsTesting + "func check[T testing.TB](t T) { t.Fatal(1) }\n" + caller}, "a/p_test.go:5"},
		{"a type parameter second in its field", map[string]string{
			"a/p_test.go": "package a" + importsTesting + "func check[U, T testing.TB](u U, t T) { t.Fatal(u) }\n" +
				"\nfunc TestP(t *testing.T) { check(t, t) }\n"}, "a/p_test.go:5"},
		{"a type parameter constrained by a pointer to one", map[string]string{
			"a/p_test.go": "package a" + importsTesting + "func check[T *testing.T](t T) { (*t).Fatal(1) }\n" + caller}, "a/p_test.go:5"},
		{"a type parameter constrained by a module interface", map[string]string{"a/tb.go": tb,
			"a/p_test.go": "package a" + importsTesting + "func check[U any, T TB](t T) { t.Fatal(1) }\n" +
				"\nfunc TestP(t *testing.T) { check[int](t) }\n"}, "a/p_test.go:5"},
		{"a type parameter constrained by an interface literal", map[string]string{
			"a/p_test.go": "package a" + importsTesting + "func check[T interface {\n\tHelper()\n\tFatal(...any)\n}](t T) {\n\tt.Fatal(1)\n}\n" +
				caller}, "a/p_test.go:5"},
		{"an interface with no Helper", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", module+"/a", "a.Fails")}, ""},
		{"a type parameter constrained otherwise", map[string]string{
			"a/p_test.go": "package a" + importsTesting + "func check[T interface{ Fatal(...any) }](t T) { t.Fatal(1) }\n" + caller}, ""},
		{"a type parameter named like a module interface", map[string]string{"a/tb.go": tb,
			"a/p_test.go": "package a" + importsTesting + "func check[TB Fails](t TB) { t.Fatal(1) }\n" + caller}, ""},
		{"a type parameter whose constraint points to one named like it", map[string]string{"a/tb.go": tb,
			"a/p_test.go": viaTestify + "func check[U *TB, TB Fails](t U) { assert.NotNil(nil, t) }" + pointee +
				"func use() { check[*f](nil) }\n"}, ""},
		{"a type parameter whose constraint points to one named like it, second in its field", map[string]string{"a/tb.go": tb,
			"a/p_test.go": viaTestify + "func check[U *TB, V, TB Fails](t U, v V) { assert.NotNil(nil, t) }" + pointee +
				"func use() { check[*f](nil, f{}) }\n"}, ""},
		{"an external test package's own type of that name", map[string]string{"a/tb.go": tb,
			"a/p_test.go": "package a_test" + importsTesting + "type TB interface{ Fatal(...any) }\n\n" +
				"func check(t TB) { t.Fatal(1) }\n" + caller}, ""},
		{"an imported directory's external test type", map[string]string{
			"a/tb.go":      "package a\n\ntype TB interface{ Fatal(...any) }\n",
			"a/tb_test.go": "package a_test\n\ntype TB interface {\n\tHelper()\n\tFatal(...any)\n}\n",
			"b/p_test.go":  fmt.Sprintf(imported, "", module+"/a", "a.TB")}, ""},
		{"a name another directory declares", map[string]string{"a/tb.go": tb,
			"b/p_test.go": "package b" + importsTesting + "func check(t TB) { t.Fatal(1) }\n" + caller}, ""},
		{"a package outside the module", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", "example.com/a", "a.TB")}, ""},
		{"a path outside the module that names a directory", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(imported, "", "a", "a.TB")}, ""},
		{"another import's type of the same name", map[string]string{"a/tb.go": tb,
			"b/p_test.go": fmt.Sprintf(`package b

import (
	"testing"

	_ "%s/a"
	c "example.com/c"
)

func check(t c.TB) { t.Fatal(1) }
`, module) + caller}, ""},
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
			assert.Empty(t, cmp.Diff(want, got), "mismatch (-want +got)")
		})
	}
}

// TestTestHelperViolations_EntryPointsAreTestFilesOwn pins that a function
// named like a test, benchmark or fuzz target is skipped only in a test file:
// the go tool runs none from another, so there it is a helper like any other.
func TestTestHelperViolations_EntryPointsAreTestFilesOwn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDocFile(t, filepath.Join(root, "a/p.go"), "package a"+importsTesting+
		"func TestSpec(t *testing.T) { t.Fatal(1) }\n\nfunc BenchmarkSpec(b *testing.B) { b.Fatal(1) }\n\n"+
		"func FuzzSeeds(f *testing.F) { f.Fatal(1) }\n")

	got, helpers, err := testHelperViolations(root)
	require.NoError(t, err)
	want := []string{
		"a/p.go:5: an asserting helper starts with its Helper call",
		"a/p.go:7: an asserting helper starts with its Helper call",
		"a/p.go:9: an asserting helper starts with its Helper call",
	}
	assert.Empty(t, cmp.Diff(want, got), "mismatch (-want +got)")
	assert.Equal(t, 3, helpers)
}

// TestTestHelperViolations_CallsCountWithinAPackage pins that a call counts from
// any file of the helper's package, and from none outside it, an external test
// package in its directory included: one helper of that name called elsewhere
// says nothing of a case body here.
func TestTestHelperViolations_CallsCountWithinAPackage(t *testing.T) {
	t.Parallel()
	const helper = "package p\n\nimport \"testing\"\n\nfunc check(t *testing.T) { t.Fatal(1) }\n"
	const caller = "package p\n\nimport \"testing\"\n\nfunc TestP(t *testing.T) { check(t) }\n"
	root := t.TempDir()
	writeDocFile(t, filepath.Join(root, "a/a_test.go"), helper)
	writeDocFile(t, filepath.Join(root, "a/b_test.go"), caller)
	writeDocFile(t, filepath.Join(root, "c/c_test.go"), helper)
	writeDocFile(t, filepath.Join(root, "d/d_test.go"), caller)
	writeDocFile(t, filepath.Join(root, "e/in_test.go"), strings.Replace(helper, "package p", "package e", 1)+
		"\nvar cases = []func(*testing.T){check}\n")
	writeDocFile(t, filepath.Join(root, "e/ext_test.go"), "package e_test"+importsTesting+
		"func check(t *testing.T) {\n\tt.Helper()\n\tt.Fatal(1)\n}\n\nfunc TestP(t *testing.T) { check(t) }\n")

	got, helpers, err := testHelperViolations(root)
	require.NoError(t, err)
	want := []string{"a/a_test.go:5: an asserting helper starts with its Helper call"}
	assert.Empty(t, cmp.Diff(want, got))
	assert.Equal(t, 4, helpers)
}

// testHelperViolations returns a line for each function under root that breaks
// TestTestHelpers_StartWithHelper, sorted, and how many asserting functions it
// read. A method is left out, as a call to one is a selector this does not
// follow. So is a function run only through a value (f := check; f(t)), which
// reads as a table's case does.
func testHelperViolations(root string) ([]string, int, error) {
	declared, err := testingTypes(root)
	if err != nil {
		return nil, 0, err
	}
	called := map[string]bool{}
	var pending []pendingHelper
	helpers := 0
	err = walkGoFiles(root, func(rel string, fset *token.FileSet, f *ast.File) {
		ft := fileTypes{dir: path.Dir(rel), pkg: f.Name.Name, names: importNames(f), declared: declared}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := calledName(call.Fun); ok {
					called[declKey(ft.dir, ft.pkg, id)] = true
				}
			}
			return true
		})
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || (strings.HasSuffix(rel, "_test.go") && isEntryPoint(fn.Name.Name)) {
				continue
			}
			through := failsThrough(fn.Body, ft.testingParams(fn), ft.names)
			if len(through) == 0 {
				continue
			}
			helpers++
			if !helpsEach(fn.Body, through) {
				pending = append(pending, pendingHelper{rel, ft.pkg, fn.Name.Name, fset.Position(fn.Pos()).Line})
			}
		}
	})
	if err != nil {
		return nil, 0, err
	}
	var violations []string
	for _, h := range pending {
		if called[declKey(path.Dir(h.rel), h.pkg, h.name)] || ast.IsExported(h.name) {
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
	rel, pkg, name string
	line           int
}

// declKey names a top-level declaration by its directory under the root, its
// package clause and its name. A directory can hold a package and its external
// test package, which share neither declarations nor calls.
func declKey(dir, pkg, name string) string {
	return dir + "/" + pkg + "." + name
}

// assertPaths are the testify packages whose functions fail a test from where
// they are called.
var assertPaths = []string{"github.com/stretchr/testify/assert", "github.com/stretchr/testify/require"}

// importNames maps each import path f names to the names it is imported
// under: its alias, or else the path's last element, which is the package's
// name for testing, testify and the module's packages, the paths this reads.
// It reads syntax and not types, so a local that shadows one of these names
// is still read as the import, and a dot import is not read: revive's
// import-shadowing and dot-imports refuse both.
func importNames(f *ast.File) map[string][]string {
	names := map[string][]string{}
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			// A parsed import path always unquotes.
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

// testingTypes returns, keyed by declKey, each type a file under root declares
// at top level that a helper can take in place of *testing.T, as
// openapitest.TB stands in for it: one isTesting reads as a testing type, such
// as an interface naming Helper or an alias or definition of testing.TB. One
// declared through a module type, by embedding it or as its alias or
// definition, is not read, as one walk reads them all.
func testingTypes(root string) (map[string]bool, error) {
	out := map[string]bool{}
	err := walkGoFiles(root, func(rel string, _ *token.FileSet, f *ast.File) {
		ft := fileTypes{names: importNames(f)}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ft.isTesting(ts.Type, nil) {
					out[declKey(path.Dir(rel), f.Name.Name, ts.Name.Name)] = true
				}
			}
		}
	})
	return out, err
}

// fileTypes is what reading a type in one file needs: the file's directory
// under the root and its package clause, what importNames read of it, and
// testingTypes' result.
type fileTypes struct {
	dir, pkg string
	names    map[string][]string
	declared map[string]bool
}

// testingParams returns the names of fn's parameters, in any position, whose
// type isTesting reads as a testing type, through fn's own type parameters. A
// blank one is left out, as nothing fails through it, and so is a variadic
// one, as its elements are not read by name.
func (ft fileTypes) testingParams(fn *ast.FuncDecl) []string {
	typeParams := ft.typeParams(fn)
	var out []string
	for _, field := range fn.Type.Params.List {
		if !ft.isTesting(field.Type, typeParams) {
			continue
		}
		for _, name := range field.Names {
			if name.Name != "_" {
				out = append(out, name.Name)
			}
		}
	}
	return out
}

// typeParams maps each of fn's type parameters to whether its constraint is a
// testing type. Every one of them is in scope in each constraint, where it
// reads as no testing type, so a constraint pointing to another type
// parameter, [U *T, T testing.TB], is not read.
func (ft fileTypes) typeParams(fn *ast.FuncDecl) map[string]bool {
	if fn.Type.TypeParams == nil {
		return nil
	}
	fields := fn.Type.TypeParams.List
	inScope := map[string]bool{}
	for _, field := range fields {
		for _, name := range field.Names {
			inScope[name.Name] = false
		}
	}
	out := make(map[string]bool, len(inScope))
	for _, field := range fields {
		byTesting := ft.isTesting(field.Type, inScope)
		for _, name := range field.Names {
			out[name.Name] = byTesting
		}
	}
	return out
}

// isTesting reports whether typ, less baseType's wrappers, is an interface
// literal that declaresHelper reads as a testing type or a name namesTesting
// does. A pointer to an interface counts, as it calls Helper as (*t).Helper().
func (ft fileTypes) isTesting(typ ast.Expr, typeParams map[string]bool) bool {
	base := baseType(typ)
	if it, ok := base.(*ast.InterfaceType); ok {
		return ft.declaresHelper(it, typeParams)
	}
	return ft.namesTesting(base, typeParams)
}

// baseType returns typ less any pointer, the parentheses gofmt keeps around
// or under one, and an instantiation. gofmt keeps them around a declared type
// and an interface's embedded one, not around a parameter's.
func baseType(typ ast.Expr) ast.Expr {
	typ = ast.Unparen(typ)
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = ast.Unparen(star.X)
	}
	switch x := typ.(type) {
	case *ast.IndexExpr:
		return x.X
	case *ast.IndexListExpr:
		return x.X
	default:
		return typ
	}
}

// declaresHelper reports whether methods names a Helper method or embeds a
// type namesTesting reads as a testing one. An embedded interface literal is
// not read, which keeps this from recursing.
func (ft fileTypes) declaresHelper(methods *ast.InterfaceType, typeParams map[string]bool) bool {
	return slices.ContainsFunc(methods.Methods.List, func(m *ast.Field) bool {
		if len(m.Names) == 0 {
			return ft.namesTesting(baseType(m.Type), typeParams)
		}
		return slices.ContainsFunc(m.Names, func(n *ast.Ident) bool { return n.Name == "Helper" })
	})
}

// namesTesting reports whether base, a type baseType returned, names one of
// testingKinds, a type testingTypes found, or a type parameter that typeParams
// maps to true; a type parameter's name hides a declared type's. A declared
// type is written bare in its own package and through an import elsewhere,
// whose clause is its path's last element.
func (ft fileTypes) namesTesting(base ast.Expr, typeParams map[string]bool) bool {
	switch x := base.(type) {
	case *ast.Ident:
		if byTesting, ok := typeParams[x.Name]; ok {
			return byTesting
		}
		return ft.declared[declKey(ft.dir, ft.pkg, x.Name)]
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		if !ok {
			return false
		}
		if slices.Contains(ft.names["testing"], pkg.Name) && slices.Contains(testingKinds, x.Sel.Name) {
			return true
		}
		for p, names := range ft.names {
			if dir, ok := moduleDir(p); ok && slices.Contains(names, pkg.Name) && ft.declared[declKey(dir, path.Base(p), x.Sel.Name)] {
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
	dir, ok := strings.CutPrefix(p, module+"/")
	if !ok {
		return "", false
	}
	return dir, true
}

// failsThrough returns those of params that body fails its test through by
// itself: a failing method of one, called or taken as a value, or a testify
// call with an argument, which fails through its first argument, or through
// params[0] when that is none of them; names is what importNames read of the
// file. A function literal's body is left out: a failure there is placed by
// whatever runs it. As this reads syntax and not data flow, a parameter copied
// into a local, a method expression such as (*testing.T).Fatal(t) and a
// testing value a struct holds are not followed.
func failsThrough(body *ast.BlockStmt, params []string, names map[string][]string) []string {
	through := map[string]bool{}
	calls := map[*ast.SelectorExpr]*ast.CallExpr{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			// A call is visited before its callee, so the callee finds its call here.
			if sel, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr); ok {
				calls[sel] = x
			}
		case *ast.SelectorExpr:
			for _, p := range selectorFails(x, calls[x], params, names) {
				through[p] = true
			}
		default:
		}
		return true
	})
	return slices.DeleteFunc(slices.Clone(params), func(p string) bool { return !through[p] })
}

// selectorFails returns those of params that sel fails through, as
// failsThrough reads it; call is the call sel is the callee of, nil when it is
// none. A testify selector fails only as the callee of a call with an
// argument: one naming a type, or a function taken as a value, fails nothing
// where it is written, and what runs the value later is not followed. A
// called function that fails nothing, such as require.New, and a conversion
// to a testify type still read as failing, as syntax cannot tell them apart.
func selectorFails(sel *ast.SelectorExpr, call *ast.CallExpr, params []string, names map[string][]string) []string {
	x, ok := receiver(sel.X)
	if !ok {
		return nil
	}
	if isAssertPackage(names, x.Name) {
		if call == nil || len(call.Args) == 0 || len(params) == 0 {
			return nil
		}
		if arg, ok := receiver(call.Args[0]); ok && slices.Contains(params, arg.Name) {
			return []string{arg.Name}
		}
		return params[:1]
	}
	if slices.Contains(params, x.Name) && slices.Contains(failingMethods, sel.Sel.Name) {
		return []string{x.Name}
	}
	return nil
}

// receiver returns the identifier x is, through parentheses and one
// dereference: a parameter that points to an interface calls its methods as
// (*t).Helper().
func receiver(x ast.Expr) (*ast.Ident, bool) {
	x = ast.Unparen(x)
	if star, ok := x.(*ast.StarExpr); ok {
		x = ast.Unparen(star.X)
	}
	id, ok := x.(*ast.Ident)
	if !ok {
		return nil, false
	}
	return id, true
}

// isAssertPackage reports whether name is what a file calls one of assertPaths.
func isAssertPackage(names map[string][]string, name string) bool {
	return slices.ContainsFunc(assertPaths, func(p string) bool { return slices.Contains(names[p], name) })
}

// helpsEach reports whether body starts with a Helper call of each of
// through, one statement each, so a failure through any of them is placed at
// the caller: Helper marks only the value it is called on.
func helpsEach(body *ast.BlockStmt, through []string) bool {
	helped := map[string]bool{}
	for _, stmt := range body.List {
		name, ok := helperCall(stmt)
		if !ok {
			break
		}
		helped[name] = true
	}
	return !slices.ContainsFunc(through, func(p string) bool { return !helped[p] })
}

// helperCall returns what stmt calls Helper on when it is a Helper call, read
// through parentheses, around the call too, and a dereference.
func helperCall(stmt ast.Stmt) (string, bool) {
	es, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return "", false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Helper" {
		return "", false
	}
	x, ok := receiver(sel.X)
	if !ok {
		return "", false
	}
	return x.Name, true
}
