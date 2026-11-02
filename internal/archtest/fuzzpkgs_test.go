// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir/irtest"
)

// fuzzSharedTables are the internal packages a fuzz package may import:
// the tables a package's other tests share with its fuzz targets, which import
// next to nothing.
var fuzzSharedTables = []string{"internal/ircases", "internal/testspec"}

// TestFuzzPackages_LinkNoTestTooling holds each fuzz package to the standard
// library, the module's pipeline packages and the shared tables, because a fuzz
// run pays for everything its binary links on every execution (#1315).
func TestFuzzPackages_LinkNoTestTooling(t *testing.T) {
	t.Parallel()
	violations, found, err := fuzzPackageViolations(irtest.RepoRoot(t))
	require.NoError(t, err)
	require.Subset(t, found, []string{"ir/fuzz", "compilers/openapi/e2e/fuzz"},
		"the walk missed a fuzz package, so it checked nothing there")
	for _, v := range violations {
		t.Error(v)
	}
}

// TestFuzzPackages_PlantedImportsAreJudged plants one import in a fuzz
// package for each kind the rule admits or refuses, and in the testdata, dot and
// underscore directories the walk leaves alone.
func TestFuzzPackages_PlantedImportsAreJudged(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		file    string
		imp     string
		refused bool
	}{
		{"testify", "ir/fuzz/x_test.go", "github.com/stretchr/testify/assert", true},
		{"a test-support package", "ir/fuzz/x_test.go", module + "/ir/irtest", true},
		{"a nested test-support package", "a/fuzz/x_test.go", module + "/compilers/openapi/internal/openapitest", true},
		{"a tooling package under internal", "ir/fuzz/x_test.go", module + "/internal/decls", true},
		{"a tooling package under internal outside testSupport", "ir/fuzz/x_test.go", module + "/internal/census", true},
		{"a program under cmd", "ir/fuzz/x_test.go", module + "/cmd/morphic", true},
		{"a shared table", "ir/fuzz/x_test.go", module + "/internal/ircases", false},
		{"the other shared table", "a/fuzz/x_test.go", module + "/internal/testspec", false},
		{"a pipeline package", "ir/fuzz/x_test.go", module + "/ir", false},
		{"a compiler's own internal package", "a/fuzz/x_test.go", module + "/compilers/openapi/internal/diag", false},
		{"the standard library", "ir/fuzz/x_test.go", "testing", false},
		{"testify in a testdata fuzz directory", "ir/testdata/fuzz/x_test.go", "github.com/stretchr/testify/assert", false},
		{"testify in a fuzz directory under a dot directory", ".claude/ref/fuzz/x_test.go", "github.com/stretchr/testify/assert", false},
		{"testify in a fuzz directory under an underscore directory", "_old/fuzz/x_test.go", "github.com/stretchr/testify/assert", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeImporter(t, filepath.Join(root, filepath.FromSlash(tc.file)), tc.imp)
			violations, _, err := fuzzPackageViolations(root)
			require.NoError(t, err)
			if !tc.refused {
				assert.Empty(t, violations)
				return
			}
			require.Len(t, violations, 1)
			assert.Contains(t, violations[0], tc.file)
			assert.Contains(t, violations[0], tc.imp)
		})
	}
}

// TestFuzzPackages_JudgeEveryImportOfEveryFileOfEveryPackage plants the refused
// import late at each level of the walk: second in its file, in the second file
// of its package, in the second package. A walk that stops after the first
// import, file or package finds less than the two violations required.
func TestFuzzPackages_JudgeEveryImportOfEveryFileOfEveryPackage(t *testing.T) {
	t.Parallel()
	const tooling = "github.com/stretchr/testify/assert"
	root := t.TempDir()
	writeImporter(t, filepath.Join(root, "a", "fuzz", "x_test.go"), tooling)
	writeImporter(t, filepath.Join(root, "ir", "fuzz", "a_test.go"), "testing")
	src := "package planted\n\nimport (\n\t_ \"testing\"\n\t_ \"" + tooling + "\"\n)\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "ir", "fuzz", "b_test.go"), []byte(src), 0o644))

	violations, found, err := fuzzPackageViolations(root)
	require.NoError(t, err)
	assert.Equal(t, []string{"a/fuzz", "ir/fuzz"}, found)
	require.Len(t, violations, 2)
	assert.Contains(t, violations[0], filepath.Join("a", "fuzz", "x_test.go"))
	assert.Contains(t, violations[1], filepath.Join("ir", "fuzz", "b_test.go"))
}

// TestFuzzPackages_OnlyFuzzDirectoriesTheGoToolReadsAreFound plants
// look-alikes of a fuzz package, which the walk must not list.
func TestFuzzPackages_OnlyFuzzDirectoriesTheGoToolReadsAreFound(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const imp = "github.com/stretchr/testify/assert"
	writeImporter(t, filepath.Join(root, "ir", "fuzz", "x_test.go"), imp)
	writeImporter(t, filepath.Join(root, "ir", "fuzzz", "x_test.go"), imp)
	writeImporter(t, filepath.Join(root, "ir", "fuzz", "prod.go"), imp)
	writeImporter(t, filepath.Join(root, "ir", "testdata", "fuzz", "x_test.go"), imp)
	writeImporter(t, filepath.Join(root, "empty", "fuzz", "prod.go"), imp)

	violations, found, err := fuzzPackageViolations(root)
	require.NoError(t, err)
	assert.Equal(t, []string{"ir/fuzz"}, found)
	assert.Len(t, violations, 1)
}

// fuzzPackageViolations walks root for fuzz packages, each a directory named
// fuzz outside any directory the go tool ignores that holds _test.go files, and
// returns the packages it found and one message for each import of theirs that
// is not admitted.
//
// Direct imports suffice: the pipeline packages admitted are held by their own
// rules entries to what they may link, and the shared tables import next to
// nothing. A package in testSupport or under the top-level internal/ or cmd/
// exists for tests or tooling and is refused, internal/decls among them for the
// golang.org/x/tools it links.
func fuzzPackageViolations(root string) (violations, found []string, err error) {
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if p != root && ignoredByGoTool(d.Name()) {
			return filepath.SkipDir
		}
		if d.Name() != "fuzz" {
			return nil
		}
		files, gerr := filepath.Glob(filepath.Join(p, "*_test.go"))
		if gerr != nil || len(files) == 0 {
			return gerr
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		found = append(found, filepath.ToSlash(rel))
		for _, file := range files {
			bad, ferr := fuzzFileViolations(file)
			if ferr != nil {
				return ferr
			}
			violations = append(violations, bad...)
		}
		return nil
	})
	return violations, found, err
}

// fuzzFileViolations returns a message for each import of one fuzz test file
// that a fuzz package may not link.
func fuzzFileViolations(file string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var violations []string
	for _, imp := range f.Imports {
		ip := strings.Trim(imp.Path.Value, `"`)
		if !fuzzMayImport(ip) {
			violations = append(violations, fmt.Sprintf(
				"%s imports %q: a fuzz package links no test tooling, only the standard library, "+
					"the pipeline's packages and %v", file, ip, fuzzSharedTables))
		}
	}
	return violations, nil
}

// fuzzMayImport reports whether a fuzz package may import ip.
func fuzzMayImport(ip string) bool {
	if first, _, _ := strings.Cut(ip, "/"); !strings.Contains(first, ".") {
		return true // stdlib: first path element has no dot
	}
	rel, inModule := strings.CutPrefix(ip, module+"/")
	if !inModule {
		return false
	}
	if slices.Contains(fuzzSharedTables, rel) {
		return true
	}
	if _, support := testSupport[rel]; support {
		return false
	}
	top, _, _ := strings.Cut(rel, "/")
	return top != "internal" && top != "cmd"
}
