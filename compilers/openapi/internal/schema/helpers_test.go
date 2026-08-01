package schema_test

import (
	"context"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// This package's tests come in two halves, and the split is not stylistic.
// Everything reachable through the exported surface lives out here, where the
// whole compiler is importable and a test can drive a spec end to end; the tests
// in package schema are the ones that read an unexported lowering directly, and
// nothing importable from here would let them. Coverage lands on the schema
// package either way: the package under test is the instrumented one, whichever
// side of the boundary the caller sits on.

// sourceOf wraps a spec string as a compilers.Source.
func sourceOf(src string) compilers.Source {
	return compilers.Source{Path: "spec.yaml", Data: []byte(src)}
}

// parseFull runs the whole public compiler pipeline over src. It reaches back
// through openapi deliberately: a schema position under a parameter or a request
// body is only hoisted once the operation walk has resolved its way down to it,
// and no amount of driving this package alone produces that.
func parseFull(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(context.Background(), []compilers.Source{sourceOf(src)},
		compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// lowerer is test scaffolding, not a compiler type. The compiler has no such
// struct: every lowering is a function of the context, the registry and the
// memos, and the caller owns the document being built (#177). Tests that drive
// one lowering in isolation still want those in one place.
type lowerer struct {
	ctx     lowering.Ctx
	out     *ir.Document
	types   *compile.Types
	diags   compile.Diags
	anchors schema.AnchorIndex
}

// loweredFor loads src and returns the fixture over it with nothing lowered yet,
// plus the load diagnostics, so a test can drive one entry point at a time.
func loweredFor(t *testing.T, src string) (*lowerer, []ir.Diagnostic) {
	t.Helper()
	loadedDoc, diags, err := load.Load(t.Context(), 0, sourceOf(src), load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc, "load returned no document: %+v", diags)
	types := compile.NewTypes(0)
	return &lowerer{
		ctx:   lowering.New(0, loadedDoc.Doc, loadedDoc.Source, lowering.GroupByTags),
		out:   &ir.Document{Types: types.Registry()},
		types: types,
	}, diags
}

// lowerSpec loads src and lowers its component schemas, returning the document
// under construction and all diagnostics.
func lowerSpec(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	l, diags := loweredFor(t, src)
	l.diags.AppendAll(schema.LowerComponentSchemas(l.ctx, l.types, &l.anchors))
	return l.out, append(diags, l.diags.List()...)
}

// newRawLowerer builds a fixture over a hand-constructed document, bypassing the
// parser so nil slice/map entries (which the parser panics on) can be exercised.
func newRawLowerer(doc *soa.OpenAPI) *lowerer {
	types := compile.NewTypes(0)
	return &lowerer{
		ctx:   lowering.New(0, doc, ir.SourceInfo{}, ""),
		out:   &ir.Document{Types: types.Registry()},
		types: types,
	}
}

// requireNoErrorDiags fails the test if any diagnostic has error severity,
// reporting the first offending diagnostic.
func requireNoErrorDiags(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	d, ok := ir.FirstError(diags)
	require.False(t, ok, "unexpected error diagnostic: %+v", d)
}

// componentSpec wraps a components/schemas block in a minimal 3.1 document.
func componentSpec(schemas string) string {
	return componentSpecVer("3.1.0", schemas)
}

// componentSpecVer wraps a components/schemas block in a minimal document of the
// given OpenAPI version.
func componentSpecVer(version, schemas string) string {
	return "openapi: " + version + "\n" +
		"info: {title: T, version: \"1\"}\n" +
		"paths: {}\n" +
		"components:\n  schemas:\n" + schemas
}

// pathsSpec wraps a paths block in a minimal 3.1 document with no components.
func pathsSpec(paths string) string {
	return "openapi: 3.1.0\n" +
		"info: {title: T, version: \"1\"}\n" +
		"paths:\n" + paths
}

// componentID is the stable TypeID of a components-named schema, or of a
// sub-schema beneath one ("Holder/properties/inner").
func componentID(name string) ir.TypeID {
	return ir.TypeID("t/openapi/components/schemas/" + name)
}

// typeByName returns the named component schema's lowered TypeDef.
func typeByName(doc *ir.Document, name string) ir.TypeDef {
	return doc.Types[componentID(name)]
}

// conflictDiags returns every conflicting-redeclaration diagnostic in diags.
func conflictDiags(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.ConflictingRedecl {
			out = append(out, d)
		}
	}
	return out
}

// emptyEitherSchema is a JSONSchema whose either-value has neither a Left schema
// nor a Right bool set: IsSchema() is true (IsLeft defaults true) yet GetSchema()
// is nil. The parser never produces this, so it drives the nil-schema guards.
func emptyEitherSchema() *oas3.JSONSchema[oas3.Referenceable] {
	return oas3.NewJSONSchemaFromSchema[oas3.Referenceable](nil)
}

// yamlNode parses a YAML snippet and returns its root value node (the
// document node's single content child), matching what schema fields expose.
func yamlNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &doc))
	require.Len(t, doc.Content, 1, "expected a single document node")
	return doc.Content[0]
}

// strNode builds a bare string-scalar yaml.Node.
func strNode(val string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val}
}

// hasDiag reports whether diags contains a diagnostic with the exact code, at
// any severity. It is the existential half of the vocabulary: use it where a
// test only needs to know a diagnostic fired, not how many or at what severity.
func hasDiag(diags []ir.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

// hasDiagAt reports whether diags contains a diagnostic with the exact code at
// the exact severity.
func hasDiagAt(diags []ir.Diagnostic, code string, sev ir.Severity) bool {
	return countDiagsAt(diags, code, sev) > 0
}

// firstDegradedWarning returns the first diag.DegradedConstruct warning in
// diags, and whether one was found — the pointer/message inspection counterpart
// to hasDiagAt/countDiagsAt.
func firstDegradedWarning(diags []ir.Diagnostic) (ir.Diagnostic, bool) {
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Severity == ir.SeverityWarning {
			return d, true
		}
	}
	return ir.Diagnostic{}, false
}

// countDiagsAt counts the diagnostics in diags matching code and sev exactly.
// code is an exact match with no wildcard: countDiagsAt(diags, "",
// ir.SeverityError) matches only diagnostics whose code is literally empty — it
// is not a way to spell "every error," and reads dangerously like one, so
// callers who want that must filter on severity alone instead.
func countDiagsAt(diags []ir.Diagnostic, code string, sev ir.Severity) int {
	var n int
	for _, d := range diags {
		if d.Code == code && d.Severity == sev {
			n++
		}
	}
	return n
}

// diagMessageAt returns the message of the single diagnostic matching code,
// severity and provenance pointer. Tests that only compare a diagnostic's code
// cannot tell two lowerings apart when both report the same code with different
// reasons, so the reason itself needs an assertable handle.
func diagMessageAt(t *testing.T, diags []ir.Diagnostic, code string, sev ir.Severity, pointer string) string {
	t.Helper()
	var found []string
	for _, d := range diags {
		if d.Code == code && d.Severity == sev && d.Provenance.Pointer == pointer {
			found = append(found, d.Message)
		}
	}
	require.Len(t, found, 1, "want exactly one %v %q at %q, got %+v", sev, code, pointer, diags)
	return found[0]
}

// indexBy builds a lookup keyed by key(item).
func indexBy[T any, K comparable](items []T, key func(T) K) map[K]T {
	out := make(map[K]T, len(items))
	for _, item := range items {
		out[key(item)] = item
	}
	return out
}

// propsByWire indexes a model's properties by wire name.
func propsByWire(props []ir.Property) map[string]ir.Property {
	return indexBy(props, func(p ir.Property) string { return p.WireName })
}
