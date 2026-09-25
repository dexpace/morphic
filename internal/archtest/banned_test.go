package archtest_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bannedImports names the packages no Go file in the module may import, tests
// included, with what to use instead.
//
// encoding/json is v1. The IR's bytes are decided by ir.Document through
// encoding/json/v2, and every v1 call runs v2 under DefaultOptionsV1: HTML
// escaping, invalid UTF-8 rewritten to U+FFFD, duplicate names accepted, names
// matched case-insensitively. That is the behaviour the move to v2 removed, so
// one v1 call reintroduces it at whatever it touches.
var bannedImports = map[string]string{
	"encoding/json": "use encoding/json/v2 and encoding/json/jsontext",
}

// TestImportGraph_NoBannedImports parses every Go file the go tool would build,
// tests included, and fails on an import bannedImports names. The layering rules
// exempt test files; this ban does not, because a test that reaches v1 checks
// the IR against a codec it no longer uses.
func TestImportGraph_NoBannedImports(t *testing.T) {
	t.Parallel()
	violations, err := bannedImportViolations(repoRoot(t))
	require.NoError(t, err)
	for _, v := range violations {
		t.Error(v)
	}
}

// TestBannedImports_PlantedImportIsCaught plants the banned import beside the two
// packages the tree must keep using. "encoding/json" is a path prefix of both,
// so a prefix match would ban them too; and a v1 import in a test file, or in a
// directory the go tool skips, is the placement a walk can get wrong either way.
func TestBannedImports_PlantedImportIsCaught(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeImporter(t, filepath.Join(root, "a", "v1_test.go"), "encoding/json")
	writeImporter(t, filepath.Join(root, "a", "v2.go"), "encoding/json/v2")
	writeImporter(t, filepath.Join(root, "a", "text.go"), "encoding/json/jsontext")
	writeImporter(t, filepath.Join(root, "a", "testdata", "fixture.go"), "encoding/json")
	writeImporter(t, filepath.Join(root, ".hidden", "skipped.go"), "encoding/json")
	writeImporter(t, filepath.Join(root, "_skipped", "skipped.go"), "encoding/json")

	violations, err := bannedImportViolations(root)
	require.NoError(t, err)
	require.Len(t, violations, 1, "exactly the test file's v1 import is caught: %v", violations)
	assert.Contains(t, violations[0], filepath.Join("a", "v1_test.go"))
}

// TestBannedImports_RootIsNeverSkipped plants a tree whose root carries a name
// the go tool skips beneath it. The walk is asked about that directory, not
// about a child of it, so the skip rule must not empty the walk before it starts.
func TestBannedImports_RootIsNeverSkipped(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "testdata")
	writeImporter(t, filepath.Join(root, "a", "v1.go"), "encoding/json")

	violations, err := bannedImportViolations(root)
	require.NoError(t, err)
	assert.Len(t, violations, 1, "the root's own name must not skip it: %v", violations)
}

// bannedImportViolations walks root for every Go file the go tool would build,
// skipping the directories it skips, and returns a message for each import
// bannedImports names.
func bannedImportViolations(root string) ([]string, error) {
	var violations []string
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
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			ip := strings.Trim(imp.Path.Value, `"`)
			if instead, banned := bannedImports[ip]; banned {
				violations = append(violations, fmt.Sprintf("%s imports %q: %s", p, ip, instead))
			}
		}
		return nil
	})
	return violations, err
}

// ignoredByGoTool reports whether the go tool skips a directory by name: testdata
// holds fixtures rather than packages, and a leading "." or "_" hides a directory
// from package patterns such as ./... — which is what keeps the gitignored
// reference checkouts under .claude out of the walk.
func ignoredByGoTool(name string) bool {
	return name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}
