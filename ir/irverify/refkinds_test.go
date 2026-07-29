package irverify

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// notReferences classifies every named string type in ir that addresses no
// document-level registry, and says why each addresses none.
//
// Classifying the whole population rather than filtering it to what looks like an
// ID is what makes the guard total, in the same spirit as integerFields in
// indices_test.go. A filter on the ID suffix admits a reference class spelled
// without one; a filter on the literal underlying ident `string` admits one
// defined from another ID type. Neither can be admitted here, because every named
// string type has to be written down one way or the other.
var notReferences = map[string]string{
	// Identities the IR does have, but that no flat, document-level registry
	// declares: these nodes are reached by position inside their owner (refKind).
	"OpID":      "an operation is a position inside its group, not a registry entry",
	"ServiceID": "a service is a position in Document.Services",
	"PropID":    "a property is a position inside its model",

	// Closed enums and scalar payloads: values, never identities.
	"AdditionalMode":  "openness of a model's property set",
	"AuthKind":        "kind of auth scheme",
	"BigVal":          "arbitrary-precision numeric literal",
	"HTTPLocation":    "where a parameter sits on the wire",
	"IdempotencyKind": "idempotency guarantee",
	"MsgDirection":    "publish or subscribe",
	"PageStrategy":    "pagination strategy",
	"UnmodeledReason": "why a construct is preserved rather than modeled",
	"PresenceKind":    "how a property represents absence",
	"PrimKind":        "which built-in scalar",
	"Severity":        "how bad a diagnostic is",
	"StreamingMode":   "shape of a streaming exchange",
	"TypeKind":        "TypeDef sum tag",
	"ValueKind":       "Value sum tag",

	// An alias is not a distinct type at run time, so refKindByType can never key
	// on one: reflection reports the type it aliases. Listing it here is the
	// conservative answer — the aliased type is classified on its own row.
	"Lifecycle": "alias of plain string; reflection sees string, which names no registry",
}

// TestStringTypes_AreAllClassified fails when ir declares a named string type
// that refKindByType neither resolves nor notReferences accounts for.
//
// collectRefs finds reference-bearing fields by reflection, so no field can be
// missed; refKindByType is the one piece still written by hand, and this is what
// stops a newly added registry from going silently unchecked.
//
// One kind of reference stays invisible here and cannot be given an exclusion
// row: one carried in a plain `string` field. It declares no named type to
// classify, and reflection sees the same `string` every free-form field is.
//
// Docs.Description is one such field — CommonMark that may carry {t:TypeID}
// cross-reference tokens for emitters to resolve — so a token naming a type no
// registry declares is reported by neither reference walk. Content.Encoding was a
// second until its keys were retyped map[PropID]PartEncoding (#134): typing them
// is what let pass.Validate resolve a key where a PropID resolves at all, against
// the properties of the model the content names rather than a document-level
// registry (the PropID row below).
func TestStringTypes_AreAllClassified(t *testing.T) {
	t.Parallel()
	resolved := map[string]bool{}
	for rt := range refKindByType {
		resolved[rt.Name()] = true
	}

	found := declaredStringTypes(t)
	require.NotEmpty(t, found, "the ir package must declare named string types")
	for _, name := range found {
		assert.True(t, resolved[name] || notReferences[name] != "",
			"ir.%s is a named string type that refKindByType does not resolve and "+
				"notReferences does not classify: name the registry it references (and "+
				"teach refKindByType to resolve it), or say why it references none", name)
	}
}

// maxTypeChain bounds the walk through defined types and aliases (the
// bounded-recursion rule). Go forbids a cycle among type declarations, so
// exceeding this means the parse went wrong, not that the IR grew deep.
const maxTypeChain = 16

// declaredStringTypes returns, in sorted order, the name of every type the ir
// package declares whose underlying type is string. Each step is followed through
// the package's own declarations, so `type GizmoID TypeID` and
// `type DoodadID = TypeID` both land on string — where matching the literal ident
// `string` sees neither.
//
// A declaration whose underlying type comes from another package (a selector such
// as json.RawMessage) is not followed: resolving that needs go/types rather than a
// parse. The ir package imports only encoding/json, and declares no string type
// through it.
func declaredStringTypes(t *testing.T) []string {
	t.Helper()
	decls := typeDecls(t)
	names := make([]string, 0, len(decls))
	for name := range decls {
		if underlyingIsString(t, decls, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// underlyingIsString reports whether name resolves to string, following each step
// through decls.
func underlyingIsString(t *testing.T, decls map[string]ast.Expr, name string) bool {
	t.Helper()
	for range maxTypeChain {
		expr, isDeclared := decls[name]
		if !isDeclared {
			return name == "string" // the chain ended at a builtin
		}
		id, isIdent := expr.(*ast.Ident)
		if !isIdent {
			return false // struct, map, slice, interface, func or selector
		}
		name = id.Name
	}
	require.Fail(t, "type chain too long", "resolving %s exceeded %d steps", name, maxTypeChain)
	return false
}

// typeDecls maps every type name the ir package's production sources declare at
// package level to the expression it is declared as. Function-local types are not
// package-level declarations and so cannot name a reference class.
func typeDecls(t *testing.T) map[string]ast.Expr {
	t.Helper()
	out := map[string]ast.Expr{}
	for _, path := range irSourceFiles(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		for _, decl := range f.Decls {
			gd, isGen := decl.(*ast.GenDecl)
			if !isGen || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, isType := spec.(*ast.TypeSpec)
				require.True(t, isType, "type decl spec is not a TypeSpec: %#v", spec)
				out[ts.Name.Name] = ts.Type
			}
		}
	}
	require.NotEmpty(t, out, "the ir package must declare types")
	return out
}

// irSourceFiles lists the ir package's non-test Go files, located relative to
// this test's own path so the result does not depend on the working directory.
func irSourceFiles(t *testing.T) []string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must report this test's path")
	dir := filepath.Join(filepath.Dir(self), "..")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	return out
}
