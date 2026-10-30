package archtest_test

import (
	"go/ast"
	"go/token"
	"maps"
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

// TestPackages_KeepTheirCommentInDocGo holds every package the module builds to
// a doc.go of its package comment and its clause alone. revive's
// package-comments says a comment exists, not where it is.
func TestPackages_KeepTheirCommentInDocGo(t *testing.T) {
	t.Parallel()
	violations, packages, err := docGoViolations(irtest.RepoRoot(t))
	require.NoError(t, err)
	require.Positive(t, packages, "the walk found no package, so it checked nothing")
	for _, v := range violations {
		t.Error(v)
	}
}

// TestDocGoViolations_PlantedShapesAreCaught plants each way a package can get
// doc.go wrong beside the shapes that must stay clean, so the test above cannot
// pass by recognizing nothing or by refusing everything.
func TestDocGoViolations_PlantedShapesAreCaught(t *testing.T) {
	t.Parallel()
	const (
		comment = "// Package p is planted.\npackage p\n"
		loose   = "a/doc.go: holds more or less than a package comment and its clause"
	)
	tests := []struct {
		name     string
		files    map[string]string
		packages int
		want     []string
	}{
		{"a comment and a clause", map[string]string{"a/doc.go": comment, "a/p.go": "package p\n"}, 1, nil},
		{"a package of tests alone", map[string]string{"a/p_test.go": "package p\n"}, 0, nil},
		{"a comment in a test file", map[string]string{"a/doc.go": comment, "a/p_test.go": comment}, 1, nil},
		{"no doc.go", map[string]string{"a/p.go": "package p\n"}, 1, []string{"a: no doc.go"}},
		{"a comment elsewhere", map[string]string{"a/doc.go": comment, "a/p.go": comment}, 1,
			[]string{"a/p.go: a package comment belongs in doc.go"}},
		{"a doc.go with no comment", map[string]string{"a/doc.go": "package p\n"}, 1, []string{loose}},
		{"a doc.go with a declaration", map[string]string{"a/doc.go": comment + "\nvar V int\n"}, 1, []string{loose}},
		{"a doc.go with an import", map[string]string{"a/doc.go": comment + "\nimport _ \"embed\"\n"}, 1, []string{loose}},
		{"a comment a blank line leaves apart", map[string]string{"a/doc.go": "// Package p.\n\npackage p\n"}, 1, []string{loose}},
		{"each package on its own", map[string]string{"a/doc.go": comment, "a/b/p.go": "package b\n"}, 2,
			[]string{"a/b: no doc.go"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for rel, src := range tc.files {
				writeDocFile(t, filepath.Join(root, rel), src)
			}
			got, packages, err := docGoViolations(root)
			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(tc.want, got))
			assert.Equal(t, tc.packages, packages)
		})
	}
}

// docGoViolations returns a line for each way a package under root breaks
// TestPackages_KeepTheirCommentInDocGo, and how many packages it read. A
// directory of tests alone is no package here: it has nothing to document.
func docGoViolations(root string) ([]string, int, error) {
	var violations []string
	built, documented := map[string]bool{}, map[string]bool{}
	err := walkGoFiles(root, func(rel string, _ *token.FileSet, f *ast.File) {
		if strings.HasSuffix(rel, "_test.go") {
			return
		}
		dir := path.Dir(rel)
		built[dir] = true
		if path.Base(rel) != "doc.go" {
			if f.Doc != nil {
				violations = append(violations, rel+": a package comment belongs in doc.go")
			}
			return
		}
		documented[dir] = true
		if f.Doc == nil || len(f.Decls) != 0 {
			violations = append(violations, rel+": holds more or less than a package comment and its clause")
		}
	})
	if err != nil {
		return nil, 0, err
	}
	for _, dir := range slices.Sorted(maps.Keys(built)) {
		if !documented[dir] {
			violations = append(violations, dir+": no doc.go")
		}
	}
	return violations, len(built), nil
}
