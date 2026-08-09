package schema_test

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
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

// parseFull runs the whole public compiler pipeline over src. It reaches back
// through openapi deliberately: a schema position under a parameter or a request
// body is only hoisted once the operation walk has resolved its way down to it,
// and no amount of driving this package alone produces that. That reach is also
// why openapitest cannot hold it — see that package's doc comment.
func parseFull(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{openapitest.SourceOf(src)}, compilers.Options{})
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
		lowering.GroupByTags, overlay.Origin{})), diags
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
	return lowererOver(lowering.New(0, doc, ir.SourceInfo{}, "", overlay.Origin{}))
}

// componentID is the stable TypeID of a components-named schema, or of a
// sub-schema beneath one ("Holder/properties/inner"). It stays a per-package
// copy for the reason openapitest's doc comment gives: a spelled-out ID belongs
// in a test file, where internal/archtest's ID-grammar sweep permits it.
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
