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
	// The yaml.v3 node vocabulary: the tag a resolved `<<` merge key carries and
	// the constructors for the node kinds a parse produces. It reaches yaml and
	// nothing else, which is what lets the view below import it rather than the
	// other way round — nodeview's own internal tests build these nodes, and an
	// internal test file cannot import a package that imports its own.
	"compilers/openapi/internal/ynode": {"gopkg.in/yaml.v3"},
	// A view over the raw source: mappings read the way the resolver reads them,
	// through aliases and `<<` merge keys. It reaches ids for the pointer
	// unescaping one lookup needs and ynode for the merge tag its key predicate
	// tests against, and is below both the scans that first wanted it and the
	// schema lowering that wants the same view.
	"compilers/openapi/internal/nodeview": {module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/ynode", "gopkg.in/yaml.v3"},
	// One walk over the decoded source tree, answering what the pre-lowering
	// refusals would otherwise each walk it to ask. It reaches nodeview for the
	// document root and nothing else: an index of what the source says is not
	// allowed to depend on what any consumer of it wants to say about that.
	"compilers/openapi/internal/sourceindex": {
		module + "/compilers/openapi/internal/nodeview", "gopkg.in/yaml.v3"},
	// The pre-lowering refusals. They read the source through nodeview and the
	// index built over it, report through diag, and reach no part of the
	// lowering — nothing here has a document to lower yet.
	"compilers/openapi/internal/scan": {module + "/ir",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/nodeview",
		module + "/compilers/openapi/internal/sourceindex", "gopkg.in/yaml.v3"},
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
	// Source-document patching: an OpenAPI Overlay applied to the decoded node
	// tree, and the attribution of what it changed. It reads bytes and nodes and
	// reports through diag, reaching ids for the pointer arithmetic that names a
	// position and nodeview for the document root — and nothing that lowers,
	// because it runs before there is anything to lower.
	"compilers/openapi/internal/overlay": {module + "/ir",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/nodeview",
		"github.com/speakeasy-api/openapi/overlay", "gopkg.in/yaml.v3"},
	// The entry side: parse, validate, resolve. It indexes the tree its one decode
	// produced through sourceindex, runs the pre-lowering refusals over that index
	// through scan, applies the caller's overlay through overlay, and reads value
	// only to tell a real numeric-literal problem from a library artifact. It
	// reaches nothing that lowers — at this point there is no document to lower.
	"compilers/openapi/internal/load": {module + "/ir", module + "/compilers",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/overlay",
		module + "/compilers/openapi/internal/scan",
		module + "/compilers/openapi/internal/sourceindex",
		module + "/compilers/openapi/internal/value",
		"github.com/speakeasy-api/openapi/jsonschema/oas3",
		"github.com/speakeasy-api/openapi/marshaller",
		"github.com/speakeasy-api/openapi/openapi",
		"github.com/speakeasy-api/openapi/validation",
		"github.com/speakeasy-api/openapi/yml", "gopkg.in/yaml.v3"},
	// What a $ref names: the pointer it addresses and the type already interned
	// there. It reaches annotation to ask whether a referenced position declares
	// a body at all, and compile for the registry it looks IDs up in. It reaches
	// nothing that lowers — following a reference far enough to lower its target
	// recurses back into the schema walk, so that stays with the walk.
	"compilers/openapi/internal/resolve": {module + "/ir", module + "/compilers/compile",
		module + "/compilers/openapi/internal/annotation",
		module + "/compilers/openapi/internal/ids",
		"github.com/speakeasy-api/openapi/jsonschema/oas3",
		"github.com/speakeasy-api/openapi/references"},
	// allOf property reconciliation. It reaches annotation for the one field a
	// redeclaration unions rather than intersects, and takes everything else it
	// needs from lowering — the registry lookup and the recorder — as function
	// fields, which is what keeps the conflict lattice drivable without a walk.
	"compilers/openapi/internal/merge": {module + "/ir",
		module + "/compilers/openapi/internal/annotation",
		module + "/compilers/openapi/internal/diag"},
	// What is being lowered: the parsed document, the identity of the source, and
	// the indexes derived from them at entry. It is the substrate both walks share
	// and so must reach neither, which is why it sits here rather than with either
	// one. It reaches load for the version grammar alone — the dialect question is
	// asked of the document, and the grammar that answers it is the loader's.
	"compilers/openapi/internal/lowering": {module + "/ir",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/load",
		module + "/compilers/openapi/internal/overlay",
		module + "/compilers/openapi/internal/resolve",
		"github.com/speakeasy-api/openapi/openapi"},
	// The security schemes a document declares and the requirements that name
	// them. It is its own package because both sides of the compiler reach it and
	// neither may reach the other: the document walk lowers the schemes, and the
	// service and operation walks lower the requirements against them.
	"compilers/openapi/internal/auth": {module + "/ir", module + "/compilers/compile",
		module + "/compilers/openapi/internal/annotation",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/lowering",
		module + "/compilers/openapi/internal/resolve",
		"github.com/speakeasy-api/openapi/openapi"},
	// The schema walk, and everything that recurses with it: composition,
	// reference following, and the preservation that hangs off both. It is one
	// package because they are one cycle (micro-compiler-design §5), and it
	// reaches every part of the compiler below it — but nothing above: an import
	// of the compiler package from here is the cycle the extraction removed.
	"compilers/openapi/internal/schema": {module + "/ir", module + "/compilers/compile",
		module + "/compilers/openapi/internal/annotation",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/lowering",
		module + "/compilers/openapi/internal/merge",
		module + "/compilers/openapi/internal/nodeview",
		module + "/compilers/openapi/internal/resolve",
		module + "/compilers/openapi/internal/value",
		"github.com/speakeasy-api/openapi/extensions",
		"github.com/speakeasy-api/openapi/jsonschema/oas3",
		"github.com/speakeasy-api/openapi/values", "gopkg.in/yaml.v3"},
	// The operation walk: path items, webhooks and callbacks, the parameters
	// merged onto them, and the content of every body, response and header. It
	// reaches the schema walk for the positions it hoists and auth for the
	// requirements it reads, and nothing above itself — the compiler assembles
	// the document from what this returns rather than the other way round.
	"compilers/openapi/internal/operation": {module + "/ir", module + "/compilers/compile",
		module + "/compilers/openapi/internal/annotation",
		module + "/compilers/openapi/internal/auth",
		module + "/compilers/openapi/internal/diag",
		module + "/compilers/openapi/internal/ids",
		module + "/compilers/openapi/internal/lowering",
		module + "/compilers/openapi/internal/resolve",
		module + "/compilers/openapi/internal/schema",
		module + "/compilers/openapi/internal/value",
		"github.com/speakeasy-api/openapi" + subtreeSuffix, "gopkg.in/yaml.v3"},
	// The scaffolding the compiler's test packages share. It is a production
	// package only in the sense that it holds non-test files; what governs it is
	// that every test package under compilers/openapi must be able to import it,
	// internal ones included. That is why its allowlist stops at ir, the contract
	// package and diag: an internal test file may not import a package that
	// imports its own, so anything further would shut out the tests of whatever
	// it reached. Widening this entry is how that becomes true silently.
	"compilers/openapi/internal/openapitest": {module + "/ir", module + "/compilers",
		module + "/compilers/openapi/internal/diag",
		"github.com/speakeasy-api/openapi/jsonschema/oas3",
		"github.com/speakeasy-api/openapi/openapi",
		"github.com/speakeasy-api/openapi/sequencedmap",
		"github.com/stretchr/testify/assert",
		"github.com/stretchr/testify/require", "gopkg.in/yaml.v3"},
	"pass": {module + "/ir"},
	// The orchestration. It reaches the compiler package to compose the default
	// registry and nothing of any source format: what a spec looks like and what
	// its options are called are answered through the contract, so no parser is
	// named here.
	"engine": {module + "/ir", module + "/compilers", module + "/compilers/openapi",
		module + "/pass"},
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

// writeImporter writes a minimal parseable Go file at file importing imp, so a
// planted tree exercises walkImports without a buildable package.
func writeImporter(t *testing.T, file, imp string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	src := "package planted\n\nimport _ \"" + imp + "\"\n"
	require.NoError(t, os.WriteFile(file, []byte(src), 0o644))
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
