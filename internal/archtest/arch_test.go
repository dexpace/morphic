package archtest_test

import (
	"fmt"
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

// subtreeSuffix marks an allowlist entry that admits a whole subtree rather than
// one package.
const subtreeSuffix = "/..."

// rules maps a directory (relative to repo root) to its allowed non-stdlib
// imports; test files are exempt. The walk starts only at keyed directories and
// recurses into their subtrees, so an unkeyed subdirectory nested under a keyed
// one is still audited, under the ancestor's allowlist.
//
// An entry is one exact import path, or a subtree when it ends in "/...".
// The distinction is what makes "compilers never import each other" expressible:
// compilers/openapi may import the contract package and the shared framework
// package, and each is named in its own right, so no sibling compiler rides in
// beside them. Prefer the exact form — a subtree entry is for an external module
// whose package layout is not ours to enumerate.
var rules = map[string][]string{
	"ir":          {},
	"ir/irtest":   {module + "/ir", "github.com/google/go-cmp" + subtreeSuffix},
	"ir/irverify": {module + "/ir"},
	"compilers":   {module + "/ir"},
	"compilers/openapi": {module + "/ir", module + "/compilers", module + "/compilers/compile",
		module + "/compilers/openapi/internal" + subtreeSuffix,
		"github.com/speakeasy-api/openapi" + subtreeSuffix, "gopkg.in/yaml.v3"},
	// The diagnostic vocabulary sits at the bottom of the compiler's own import
	// graph: every package that reports anything reaches it, so it must reach
	// nothing but ir. Its own entry says that, rather than letting it inherit the
	// compiler's much wider allowlist by being an unkeyed subdirectory.
	"compilers/openapi/internal/diag": {module + "/ir"},
	// Pointer arithmetic and the OpenAPI derivation of an ID from a pointer. It
	// reaches the framework for the grammar wrapped around a path and nothing
	// else: what is OpenAPI's here is the path, and a package that could reach
	// the compiler would be able to derive one from something other than a
	// source coordinate.
	"compilers/openapi/internal/ids": {module + "/ir", module + "/compilers/compile"},
	// Scalar and BigVal lowering. It reaches ir for the value types and yaml for
	// the node it reads, and nothing else: a value is decided by its own wire
	// spelling, so a package that could reach the compiler would be able to let a
	// surrounding schema type change what a literal means.
	"compilers/openapi/internal/value": {module + "/ir", "gopkg.in/yaml.v3"},
	// A view over the raw source: mappings read the way the resolver reads them,
	// through aliases and `<<` merge keys. It reaches ids for the pointer
	// unescaping one lookup needs, and is below both the scans that first wanted
	// it and the schema lowering that wants the same view.
	"compilers/openapi/internal/nodeview": {module + "/compilers/openapi/internal/ids", "gopkg.in/yaml.v3"},
	// The pre-lowering refusals. They read the source through nodeview and report
	// through diag, and reach no part of the lowering — nothing here has a
	// document to lower yet.
	"compilers/openapi/internal/scan": {module + "/ir",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/nodeview", "gopkg.in/yaml.v3"},
	// What a schema or a carrier says about itself rather than about its shape,
	// plus the validation-only keywords the IR keeps verbatim. It reads the
	// parsed model and the raw nodes behind it, and holds no opinion about
	// lowering — so it sits below the schema lowering that asks it questions and
	// above nothing that asks it any.
	"compilers/openapi/internal/annotation": {module + "/ir",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/value",
		"github.com/speakeasy-api/openapi/extensions",
		"github.com/speakeasy-api/openapi/jsonschema/oas3", "gopkg.in/yaml.v3"},
	"pass": {module + "/ir"},
	"engine": {module + "/ir", module + "/compilers", module + "/compilers/openapi",
		module + "/pass", "gopkg.in/yaml.v3"},
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
			violations, err := walkImports(dir, base, allowed)
			require.NoError(t, err)
			for _, v := range violations {
				t.Error(v)
			}
		})
	}
}

// TestImportGraph_ContractEntryAdmitsNoSiblingCompiler states the rule the
// allowlist exists for, at the matcher rather than through the tree: a compiler
// reaches the contract package and the shared framework package by naming each,
// and no sibling compiler comes with them.
//
// It is asked of the live rules entry, not a fixture, because the hole was in
// how that entry was read: every path here was allowed while the match was a
// segment prefix, so a compiler could import another and the suite stayed green
// (GitHub #57).
func TestImportGraph_ContractEntryAdmitsNoSiblingCompiler(t *testing.T) {
	t.Parallel()
	allowed := rules["compilers/openapi"]
	require.NotEmpty(t, allowed, "rules must still carry the compilers/openapi entry")
	assert.True(t, importAllowed(module+"/compilers", allowed), "the contract package")
	assert.True(t, importAllowed(module+"/compilers/compile", allowed), "the shared framework package")
	assert.True(t, importAllowed("github.com/speakeasy-api/openapi/jsonschema/oas3", allowed),
		"a subtree entry still admits the packages under it")
	assert.False(t, importAllowed(module+"/compilers/graphql", allowed),
		"a sibling compiler must not ride in on the contract entry")
	assert.False(t, importAllowed(module+"/engine", allowed), "nor a layer above")
}

// TestWalkImports_NestedLookAlikeIsAudited plants the tree the skip decision
// used to miss: a directory whose basename matches a ruled sibling's but whose
// path carries no rule of its own. Skipping it left it audited by nothing at
// all, while TestImportGraph_EveryPackageIsRuledOrExempt still reported it
// covered — its ancestor is ruled (GitHub #57).
//
// Both directories are planted so the fix cannot be "stop skipping": the real
// ir/irtest child must still be skipped for its own rule, or it would be judged
// by ir's empty allowlist and fail for imports it is entitled to.
func TestWalkImports_NestedLookAlikeIsAudited(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeImporter(t, filepath.Join(root, "sub", "irtest", "nested.go"), "github.com/google/go-cmp/cmp")
	writeImporter(t, filepath.Join(root, "irtest", "child.go"), "github.com/google/go-cmp/cmp")

	violations, err := walkImports("ir", root, rules["ir"])
	require.NoError(t, err)
	require.Len(t, violations, 1, "exactly the nested look-alike is audited: %v", violations)
	assert.Contains(t, violations[0], filepath.Join("sub", "irtest", "nested.go"))
}

// writeImporter writes a minimal parseable Go file at path importing imp, so a
// planted tree exercises walkImports without a buildable package.
func writeImporter(t *testing.T, path, imp string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	src := "package planted\n\nimport _ \"" + imp + "\"\n"
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
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

// walkImports traverses base and returns one message per production Go file
// import that allowed does not cover. It excludes nested directories that carry
// their own rule so each subtree is checked exactly once under its most specific
// rule.
//
// It returns violations rather than reporting them, so the planted-tree tests
// above can assert what a walk finds instead of only that a real walk is clean.
func walkImports(dir, base string, allowed []string) ([]string, error) {
	var violations []string
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return skipIfRuled(dir, base, p)
		}
		if !isProductionGoFile(d.Name()) {
			return nil
		}
		found, ferr := fileViolations(p, dir, allowed)
		if ferr != nil {
			return ferr
		}
		violations = append(violations, found...)
		return nil
	})
	return violations, err
}

// skipIfRuled reports filepath.SkipDir for a directory carrying its own rules
// entry, which walkImports visits under that entry instead.
//
// The entry is looked up by the directory's full path under dir, not by its
// basename: with a basename, *any* directory at any depth sharing a ruled
// sibling's name was skipped — and skipped by a walk that never reached it under
// its own rule either, so nothing audited it (GitHub #57).
func skipIfRuled(dir, base, p string) error {
	if p == base {
		return nil
	}
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return err
	}
	if _, own := rules[path.Join(dir, filepath.ToSlash(rel))]; own {
		return filepath.SkipDir
	}
	return nil
}

// fileViolations parses one file's import declarations and returns a message for
// each disallowed non-stdlib import.
func fileViolations(p, dir string, allowed []string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var violations []string
	for _, imp := range f.Imports {
		ip := strings.Trim(imp.Path.Value, `"`)
		if !strings.Contains(strings.SplitN(ip, "/", 2)[0], ".") {
			continue // stdlib: first path element has no dot
		}
		// An empty allowlist (the "ir" rule) forbids every non-stdlib import.
		if !importAllowed(ip, allowed) {
			violations = append(violations,
				fmt.Sprintf("%s imports %q: not allowed for %s (allowed: %v)", p, ip, dir, allowed))
		}
	}
	return violations, nil
}

// importAllowed reports whether the import path ip is covered by allowed: an
// entry matches ip exactly, or — written with the subtree suffix — matches ip
// and everything under it. Matching a subtree on segment boundaries is what
// keeps "ir" from admitting "irtest".
func importAllowed(ip string, allowed []string) bool {
	for _, entry := range allowed {
		root, subtree := strings.CutSuffix(entry, subtreeSuffix)
		if ip == root {
			return true
		}
		if subtree && strings.HasPrefix(ip, root+"/") {
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
