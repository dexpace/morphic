// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir/irtest"
)

// pendingTag is the build constraint a pending test file carries: no gate
// runs it, and `make vet` compiles it.
const pendingTag = "//go:build pending"

// issueCitation is how a comment cites an issue, as the repository's comments
// do: the doc comment may wrap between the two words.
var issueCitation = regexp.MustCompile(`GitHub\s+#[0-9]+`)

// TestPendingTests_EachCitesAnIssue holds each `_pending_test.go` file to the
// `//go:build pending` line and each test it declares to the issue it waits
// for, cited as `GitHub #N` in its doc comment, so a test that fails by design
// is one an open issue explains and the change closing that issue knows to
// untag.
func TestPendingTests_EachCitesAnIssue(t *testing.T) {
	t.Parallel()
	problems, tests, err := pendingProblems(irtest.RepoRoot(t))
	require.NoError(t, err)
	require.NotZero(t, tests, "found no pending test, so this proves nothing")
	for _, p := range problems {
		t.Error(p)
	}
}

// pendingProblems reports each pending test file under root without the
// pending build constraint and each test in one whose doc comment cites no
// issue, and counts the tests it read.
func pendingProblems(root string) ([]string, int, error) {
	var problems []string
	tests := 0
	err := walkGoFiles(root, func(rel string, fset *token.FileSet, f *ast.File) {
		if !strings.HasSuffix(rel, "_pending_test.go") {
			return
		}
		if !hasPendingTag(f) {
			problems = append(problems, fmt.Sprintf("%s does not open with %s", rel, pendingTag))
		}
		testingNames := importNames(f)["testing"]
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !takesTestingT(fn, testingNames) {
				continue
			}
			tests++
			if fn.Doc == nil || !issueCitation.MatchString(fn.Doc.Text()) {
				problems = append(problems, fmt.Sprintf("%s:%d: %s cites no issue: write GitHub #<n> in its doc comment",
					rel, fset.Position(fn.Pos()).Line, fn.Name.Name))
			}
		}
	})
	return problems, tests, err
}

// hasPendingTag reports whether a comment above f's package clause is the
// pending build constraint.
func hasPendingTag(f *ast.File) bool {
	for _, group := range f.Comments {
		if group.Pos() >= f.Package {
			return false
		}
		for _, c := range group.List {
			if c.Text == pendingTag {
				return true
			}
		}
	}
	return false
}

// TestPendingProblems_ReportsWhatAPendingTestOmits plants a pending file with
// a cited test, one citing across a line break, one citing nothing and a
// helper, a pending file without the constraint, and an ordinary test file
// that cites nothing and carries no constraint.
func TestPendingProblems_ReportsWhatAPendingTestOmits(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDocFile(t, filepath.Join(root, "a", "a_pending_test.go"), `// Header.

//go:build pending

package a

import "testing"

// TestCited waits (GitHub #7).
func TestCited(t *testing.T) {}

// TestWrapped waits (GitHub
// #8).
func TestWrapped(t *testing.T) {}

// TestUncited waits for nothing.
func TestUncited(t *testing.T) {}

func TestBare(t *testing.T) {}

func helper(t *testing.T) {}
`)
	writeDocFile(t, filepath.Join(root, "b", "b_pending_test.go"), `package b

import "testing"

// TestUntagged waits (GitHub #9).
func TestUntagged(t *testing.T) {}
`)
	writeDocFile(t, filepath.Join(root, "c", "c_test.go"), "package c\n\nimport \"testing\"\n\nfunc TestOrdinary(t *testing.T) {}\n")

	problems, tests, err := pendingProblems(root)
	require.NoError(t, err)
	assert.Equal(t, 5, tests, "each test of a pending file is counted, and no other file is read")
	assert.Equal(t, []string{
		"a/a_pending_test.go:17: TestUncited cites no issue: write GitHub #<n> in its doc comment",
		"a/a_pending_test.go:19: TestBare cites no issue: write GitHub #<n> in its doc comment",
		"b/b_pending_test.go does not open with //go:build pending",
	}, problems)
}
