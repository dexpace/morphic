package archtest_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const module = "github.com/dexpace/morphic"

// rules maps a directory (relative to repo root) to its allowed non-stdlib
// import prefixes. Test files are exempt; layering applies to production code.
//
// engine and cmd/morphic do not exist yet (Phase 4). Their rules are declared
// here so the assertion is ready the moment those packages land; absent
// directories are skipped by the walk (see TestImportGraph_LayeringHolds).
var rules = map[string][]string{
	"ir":                  {},
	"ir/irtest":           {module + "/ir", "github.com/google/go-cmp"},
	"ir/irverify":         {module + "/ir"},
	"compilers":           {module + "/ir"},
	"compilers/openapi":   {module + "/ir", module + "/compilers", "github.com/speakeasy-api/openapi", "gopkg.in/yaml.v3"},
	"pass":                {module + "/ir"},
	"engine":              {module + "/ir", module + "/compilers", module + "/pass", "gopkg.in/yaml.v3"},
	"cmd/morphic":         {module + "/ir", module + "/engine"},
	"cmd/morphic-harness": {module + "/internal/harness"},
}

// TestImportGraph_LayeringHolds parses every non-test Go file under each ruled
// directory (imports only) and fails on any import whose path is neither stdlib
// nor covered by that directory's allowlist.
func TestImportGraph_LayeringHolds(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for dir, allowed := range rules {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			base := filepath.Join(root, dir)
			if _, err := os.Stat(base); os.IsNotExist(err) {
				t.Skipf("directory %s does not exist yet", dir)
			}
			require.NoError(t, walkImports(t, dir, base, allowed))
		})
	}
}

// walkImports traverses base, checking every production Go file's imports
// against allowed. It excludes nested directories that carry their own rule so
// each subtree is checked exactly once under its most specific rule.
func walkImports(t *testing.T, dir, base string, allowed []string) error {
	t.Helper()
	return filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != base && hasOwnRule(dir, filepath.Base(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		checkFileImports(t, path, dir, allowed)
		return nil
	})
}

// checkFileImports parses one file's import declarations and reports any
// disallowed non-stdlib import via t.Errorf.
func checkFileImports(t *testing.T, path, dir string, allowed []string) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	require.NoError(t, err)
	for _, imp := range f.Imports {
		ip := strings.Trim(imp.Path.Value, `"`)
		if !strings.Contains(strings.SplitN(ip, "/", 2)[0], ".") {
			continue // stdlib: first path element has no dot
		}
		// An empty allowlist (the "ir" rule) forbids every non-stdlib import.
		if !hasAllowedPrefix(ip, allowed) {
			t.Errorf("%s imports %q: not allowed for %s (allowed: %v)", path, ip, dir, allowed)
		}
	}
}

// hasOwnRule reports whether the child directory named child, nested directly
// under dir, has its own entry in rules and must therefore be walked under that
// entry instead of the parent's.
func hasOwnRule(dir, child string) bool {
	_, ok := rules[dir+"/"+child]
	return ok
}

// hasAllowedPrefix reports whether the import path ip is covered by any allowed
// prefix, matching on path-segment boundaries so "ir" never matches "irtest".
func hasAllowedPrefix(ip string, allowed []string) bool {
	for _, prefix := range allowed {
		if ip == prefix || strings.HasPrefix(ip, prefix+"/") {
			return true
		}
	}
	return false
}

// repoRoot walks up from this test file's directory until it finds the module's
// go.mod, returning that directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "reached filesystem root without finding go.mod")
		dir = parent
	}
}
