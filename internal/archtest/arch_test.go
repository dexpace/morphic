package archtest_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const module = "github.com/dexpace/morphic"

// rules maps a directory (relative to repo root) to its allowed non-stdlib
// import prefixes; test files are exempt. The walk starts only at keyed
// directories and recurses into their subtrees, so an unkeyed subdirectory
// nested under a keyed one is still audited, under the ancestor's allowlist.
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
	"internal/testspec":   {},
}

// exempt names the production packages deliberately outside the layering rules,
// each with the reason it is out.
//
// Being unkeyed in rules is not by itself a decision — an omission looks
// identical to one — so TestImportGraph_EveryPackageIsRuledOrExempt requires
// every production package to appear in one map or the other. Adding a package
// to neither fails, which is what stops both lists rotting.
var exempt = map[string]string{
	"internal/harness": "test/tooling infrastructure that drives specs through the oracles, " +
		"so it legitimately imports the Layer-1 compilers/openapi package",
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

// TestImportGraph_EveryPackageIsRuledOrExempt requires every directory holding
// production Go files to be reached by rules — through itself or an ancestor —
// or named in exempt with its reason.
//
// A package in neither is audited by nothing, and looks no different from one
// deliberately left out: the layering gate stays green while that package
// imports whatever it likes. Requiring the choice to be recorded is what makes
// the next package added an explicit decision rather than a silent omission.
func TestImportGraph_EveryPackageIsRuledOrExempt(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	pkgs := productionPackages(t, root)
	require.NotEmpty(t, pkgs, "found no production packages to audit")

	for _, dir := range pkgs {
		if _, ok := exempt[dir]; ok {
			assert.False(t, coveredByRule(dir), "%s is both ruled and exempt; pick one", dir)
			continue
		}
		assert.True(t, coveredByRule(dir),
			"%s holds production Go files but no import rule reaches it; "+
				"add it to rules, or name it in exempt with the reason it is out", dir)
	}
	for dir := range exempt {
		assert.Contains(t, pkgs, dir, "exempt names %s, which holds no production Go files", dir)
	}
}

// coveredByRule reports whether dir or one of its ancestors carries an import
// rule, which is what decides whether walkImports ever reaches it.
func coveredByRule(dir string) bool {
	for d := dir; ; d = path.Dir(d) {
		if _, ok := rules[d]; ok {
			return true
		}
		if d == "." || d == "/" {
			return false
		}
	}
}

// productionPackages returns every repo-relative directory holding at least one
// non-test Go file, deduplicated and in lexical order. Dot-directories are
// skipped whole, so neither .git nor tooling state reaches the audit.
func productionPackages(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !isProductionGoFile(d.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(root, filepath.Dir(p))
		require.NoError(t, relErr)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	require.NoError(t, err)
	slices.Sort(out)
	return slices.Compact(out)
}

// isProductionGoFile reports whether name is a Go file the layering rules apply
// to. walkImports exempts test files, so the package sweep must exempt them too
// — otherwise a test-only directory would be required to carry a rule that
// governs none of its files.
func isProductionGoFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
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
		if !isProductionGoFile(d.Name()) {
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
