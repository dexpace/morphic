// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/keydoc"
	"github.com/dexpace/morphic/ir/irtest"
)

// testNameSpan is a span of code that is a test's name alone, optionally
// qualified by its package's name and followed by a subtest path, which is not
// read.
var testNameSpan = regexp.MustCompile(`^(?:([a-z][a-z0-9]*)\.)?(Test[A-Za-z0-9_]*)(?:/\S*)?$`)

// testNamePlaceholders are the spans written as a test's name that stand for
// no test, each with the reason.
var testNamePlaceholders = map[string]string{
	"TestFunc_Scenario": "the root guidance's naming convention, spelled as a shape",
	"package.TestName":  "the lowering specification's citation form, spelled as a shape",
}

// TestDocuments_NameOnlyTestsThatExist reads every Markdown document of the
// repository and fails on a span of code naming a test no test file declares,
// or, written as pkg.TestName, none of package pkg's does. The documents send
// a reader to a test by name — the contributor guide's steps each name the
// check that fails when the step is missed — so a test renamed or moved
// without them sends the reader nowhere.
func TestDocuments_NameOnlyTestsThatExist(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	declared, err := declaredTests(root)
	require.NoError(t, err)
	require.NotEmpty(t, declared, "found no test, so this proves nothing")
	problems, named, err := testNameProblems(root, declared)
	require.NoError(t, err)
	require.NotZero(t, named, "found no test named in a document, so this proves nothing")
	for _, p := range problems {
		t.Error(p)
	}
}

// testNameProblems reports each test a Markdown document under root names
// that declared lacks, and counts the names it read. A span naming a family by
// its prefix, `TestLoweringSpec` for the TestLoweringSpec_ tests, names a test
// when one is declared in that family.
func testNameProblems(root string, declared map[string]map[string]bool) ([]string, int, error) {
	var problems []string
	named := 0
	err := keydoc.Walk(root, ".", func(path string) error {
		rel := keydoc.Rel(root, path)
		if filepath.Ext(path) != ".md" || slices.Contains(strings.Split(rel, "/"), "testdata") {
			return nil
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(text), "\n") {
			for _, span := range keydoc.Spans(line) {
				m := testNameSpan.FindStringSubmatch(span)
				if m == nil || testNamePlaceholders[strings.TrimPrefix(m[1]+"."+m[2], ".")] != "" {
					continue
				}
				named++
				if declaresTest(declared, m[1], m[2]) {
					continue
				}
				which := "no test file"
				if m[1] != "" {
					which = "no test file of package " + m[1]
				}
				problems = append(problems, fmt.Sprintf("%s:%d names %q, which %s declares", rel, i+1, span, which))
			}
		}
		return nil
	})
	return problems, named, err
}

// declaresTest reports whether declared holds name, or a test in the family
// name prefixes, in package pkg, or in any package when pkg is empty.
func declaresTest(declared map[string]map[string]bool, pkg, name string) bool {
	for d, pkgs := range declared {
		if d != name && !strings.HasPrefix(d, name+"_") {
			continue
		}
		if pkg == "" || pkgs[pkg] {
			return true
		}
	}
	return false
}

// declaredTests returns the name of every test function the test files under
// root declare, a top-level Test… taking a *testing.T alone, with the names of
// the packages declaring it. An external test package counts as the package it
// tests, the name a document qualifies its tests by.
func declaredTests(root string) (map[string]map[string]bool, error) {
	out := map[string]map[string]bool{}
	err := walkGoFiles(root, func(rel string, _ *token.FileSet, f *ast.File) {
		if !strings.HasSuffix(rel, "_test.go") {
			return
		}
		pkg := strings.TrimSuffix(f.Name.Name, "_test")
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && takesTestingT(fn) {
				if out[fn.Name.Name] == nil {
					out[fn.Name.Name] = map[string]bool{}
				}
				out[fn.Name.Name][pkg] = true
			}
		}
	})
	return out, err
}

// takesTestingT reports whether fn is a test function: TestName(t *testing.T).
func takesTestingT(fn *ast.FuncDecl) bool {
	if fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") || fn.Type.Params.NumFields() != 1 {
		return false
	}
	star, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing" && sel.Sel.Name == "T"
}

// TestTestNameProblems_ReportsANameNoTestDeclares plants names in Markdown a
// test declares, a family prefix, a subtest, a placeholder, a name inside a
// longer span, and names nothing declares, in a document and under testdata,
// each bare and qualified by its package, the wrong one and one no file has.
func TestTestNameProblems_ReportsANameNoTestDeclares(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDocFile(t, filepath.Join(root, "a", "a_test.go"),
		"package a\n\nimport \"testing\"\n\nfunc TestReal(t *testing.T) {}\n\n"+
			"func TestFamily_One(t *testing.T) {}\n\nfunc TestHelper() {}\n")
	writeDocFile(t, filepath.Join(root, "a", "b.go"), "package a\n\nfunc TestNotATest(t *testing.T) {}\n")
	writeDocFile(t, filepath.Join(root, "b", "b_test.go"),
		"package b_test\n\nimport \"testing\"\n\nfunc TestOther(t *testing.T) {}\n")
	writeDocFile(t, filepath.Join(root, "GUIDE.md"), "Run `TestReal`, `TestFamily`, `TestReal/sub` and\n"+
		"`TestFunc_Scenario`; `go test -run TestGone` is a command.\n`TestGone` and `TestHelper` and `TestNotATest`.\n"+
		"`a.TestReal`, `a.TestFamily`, `b.TestOther/sub` and `package.TestName`.\n"+
		"`b.TestReal` and `c.TestReal`.\n")
	writeDocFile(t, filepath.Join(root, "x", "testdata", "fixture.md"), "`TestUnread`\n")
	writeDocFile(t, filepath.Join(root, ".hidden", "h.md"), "`TestUnreadHidden`\n")

	declared, err := declaredTests(root)
	require.NoError(t, err)
	assert.Equal(t, map[string]map[string]bool{
		"TestReal":       {"a": true},
		"TestFamily_One": {"a": true},
		"TestOther":      {"b": true},
	}, declared, "a test is recorded under its package, an external test package's under the package it tests")

	problems, named, err := testNameProblems(root, declared)
	require.NoError(t, err)
	assert.Equal(t, 11, named, "each name read is counted once")
	assert.Equal(t, []string{
		`GUIDE.md:3 names "TestGone", which no test file declares`,
		`GUIDE.md:3 names "TestHelper", which no test file declares`,
		`GUIDE.md:3 names "TestNotATest", which no test file declares`,
		`GUIDE.md:5 names "b.TestReal", which no test file of package b declares`,
		`GUIDE.md:5 names "c.TestReal", which no test file of package c declares`,
	}, problems)

	_, _, err = testNameProblems(filepath.Join(root, "absent"), declared)
	require.ErrorIs(t, err, fs.ErrNotExist)
}
