package schema

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
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

// nestedAnchor builds a mapping chain of the given depth with a $dynamicAnchor
// at the bottom. It is built rather than parsed because nesting this deep in
// source text is indentation arithmetic, and duplicate keys at one level would
// not nest at all.
func nestedAnchor(depth int) *yaml.Node {
	n := &yaml.Node{
		Kind:    yaml.MappingNode,
		Content: []*yaml.Node{openapitest.StrNode("$dynamicAnchor"), openapitest.StrNode("toodeep")},
	}
	for range depth + 1 {
		n = &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{openapitest.StrNode("k"), n}}
	}
	return n
}

// useValue parses src — an anchor plus a `use:` key written against it — and
// returns the raw node at `use` undereferenced, so a value spelled `*anchor`
// arrives as the alias node a branch reader is really handed rather than as the
// mapping it stands for.
func useValue(t *testing.T, src string) *yaml.Node {
	t.Helper()
	node := annotation.RawChildNode(openapitest.YAMLNode(t, src), "use")
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

// lowererOver is the only place the fixture's fields are initialised. Both
// entry points below build on it, so a field added to lowerer cannot reach one
// of them and miss the other.
func lowererOver(ctx lowering.Ctx) *lowerer {
	types := compile.NewTypes(0)
	return &lowerer{
		ctx:   ctx,
		out:   &ir.Document{Types: types.Registry()},
		types: types,
	}
}

// loweredFor loads src and returns the fixture over it with nothing lowered yet,
// plus the load diagnostics, so a test can drive one entry point at a time.
func loweredFor(t *testing.T, src string) (*lowerer, []ir.Diagnostic) {
	t.Helper()
	loadedDoc, diags, err := load.Load(t.Context(), 0, openapitest.SourceOf(src), load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc, "load returned no document: %+v", diags)
	return lowererOver(lowering.New(0, loadedDoc.Doc, loadedDoc.Source,
		lowering.GroupByTags, lowering.Limits{}, lowering.StreamingMedia{}, overlay.Origin{})), diags
}

// lowerSpec loads src and lowers its component schemas, returning the document
// under construction and all diagnostics.
func lowerSpec(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	l, diags := loweredFor(t, src)
	l.diags.AppendAll(LowerComponentSchemas(t.Context(), l.ctx, l.types, &l.anchors))
	return l.out, append(diags, l.diags.List()...)
}

// newRawLowerer builds a fixture over a hand-constructed document, bypassing the
// parser so nil slice/map entries (which the parser panics on) can be exercised.
func newRawLowerer(doc *soa.OpenAPI) *lowerer {
	return lowererOver(lowering.New(0, doc, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, overlay.Origin{}))
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
