package schema

import (
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// The tests in this file and beside it are the ones that read an unexported
// lowering directly. Everything reachable through the exported surface lives in
// package schema_test, which can import the whole compiler and drive a spec end
// to end; nothing importable from there would reach these.

// deepPointer is a sub-schema pointer (not a top-level component pointer), so it
// exercises the interning-lookup paths rather than the named-component path.
const deepPointer = "/components/schemas/Obj/properties/inner"

// sourceOf wraps a spec string as a compilers.Source.
func sourceOf(src string) compilers.Source {
	return compilers.Source{Path: "spec.yaml", Data: []byte(src)}
}

// docDeclaring builds a document declaring the named component schemas, with no
// parser and no fixture — the shape a test wants when what it needs from the
// document is only which components it declares.
func docDeclaring(names ...string) *soa.OpenAPI {
	elems := make([]*sequencedmap.Element[string, *oas3.JSONSchema[oas3.Referenceable]], 0, len(names))
	for _, n := range names {
		elems = append(elems, sequencedmap.NewElem(n,
			oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})))
	}
	return &soa.OpenAPI{Components: &soa.Components{Schemas: sequencedmap.New(elems...)}}
}

// nestedAnchor builds a mapping chain of the given depth with a $dynamicAnchor
// at the bottom. It is built rather than parsed because nesting this deep in
// source text is indentation arithmetic, and duplicate keys at one level would
// not nest at all.
func nestedAnchor(depth int) *yaml.Node {
	n := &yaml.Node{
		Kind:    yaml.MappingNode,
		Content: []*yaml.Node{strNode("$dynamicAnchor"), strNode("toodeep")},
	}
	for range depth + 1 {
		n = &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{strNode("k"), n}}
	}
	return n
}

// useValue parses src — an anchor plus a `use:` key written against it — and
// returns the raw node at `use` undereferenced, so a value spelled `*anchor`
// arrives as the alias node a branch reader is really handed rather than as the
// mapping it stands for.
func useValue(t *testing.T, src string) *yaml.Node {
	t.Helper()
	node := annotation.RawChildNode(yamlNode(t, src), "use")
	require.NotNil(t, node, `src writes no "use" key`)
	return node
}

// lowerer is test scaffolding, not a compiler type. The compiler has no such
// struct: every lowering is a function of the context, the registry and the
// memos, and the caller owns the document being built (#177).
type lowerer struct {
	ctx     lowering.Ctx
	out     *ir.Document
	types   *compile.Types
	diags   compile.Diags
	anchors AnchorIndex
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
		ctx:   lowering.New(0, loadedDoc.Doc, loadedDoc.Source, lowering.GroupByTags, lowering.ExtensionPromotions{}, overlay.Origin{}),
		out:   &ir.Document{Types: types.Registry()},
		types: types,
	}, diags
}

// lowerSpec loads src and lowers its component schemas, returning the document
// under construction and all diagnostics.
func lowerSpec(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	l, diags := loweredFor(t, src)
	l.diags.AppendAll(LowerComponentSchemas(l.ctx, l.types, &l.anchors))
	return l.out, append(diags, l.diags.List()...)
}

// newRawLowerer builds a fixture over a hand-constructed document, bypassing the
// parser so nil slice/map entries (which the parser panics on) can be exercised.
func newRawLowerer(doc *soa.OpenAPI) *lowerer {
	types := compile.NewTypes(0)
	return &lowerer{
		ctx:   lowering.New(0, doc, ir.SourceInfo{}, "", lowering.ExtensionPromotions{}, overlay.Origin{}),
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
//
// The version is inlined where package schema_test's copy takes it as a
// parameter, because nothing on this side varies it. The two produce the same
// document for 3.1.0, which is what any fixture compared across the two halves
// depends on.
func componentSpec(schemas string) string {
	return "openapi: 3.1.0\n" +
		"info: {title: T, version: \"1\"}\n" +
		"paths: {}\n" +
		"components:\n  schemas:\n" + schemas
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

// assertInternalInvariant requires diags to report a broken internal invariant.
// It is the only diagnostic this half of the suite asserts by code, and that is
// not a coincidence: these tests drive the states no source document can reach,
// which is exactly what that code is for.
func assertInternalInvariant(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		if d.Code == diag.InternalInvariant && d.Severity == ir.SeverityError {
			return
		}
	}
	t.Fatalf("expected an internal-invariant error, got %+v", diags)
}
