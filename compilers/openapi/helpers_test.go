package openapi

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/operation"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// parseFull runs the whole public compiler pipeline over src.
//
// It is one of the four copies openapitest cannot hold: a helper that drives the
// compiler has to import it, and this package's own internal tests could then
// not import openapitest at all. The four are written identically so a reader
// comparing them finds no difference to account for.
func parseFull(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := New().Compile(t.Context(),
		[]compilers.Source{openapitest.SourceOf(src)}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// loweredFor loads src and returns the lowerer over it with nothing lowered
// yet, plus the load diagnostics, so a test can drive one entry point at a
// time. lowerSpec builds on this same load path to also lower components.
func loweredFor(t *testing.T, src string) (*lowerer, []ir.Diagnostic) {
	t.Helper()
	loadedDoc, diags, err := load.Load(t.Context(), 0,
		compilers.Source{Path: "spec.yaml", Data: []byte(src)}, loadOptions(Options{}.withDefaults()))
	require.NoError(t, err)
	require.NotNil(t, loadedDoc, "load returned no document: %+v", diags)
	return newLowerer(loadedDoc, Options{}.withDefaults()), diags
}

// lowerSpec loads src and lowers its component schemas, returning the document
// under construction and all diagnostics. It drives the same lowerer Compile
// will use, without requiring the not-yet-written operation lowering.
func lowerSpec(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	l, diags := loweredFor(t, src)
	l.diags.AppendAll(schema.LowerComponentSchemas(t.Context(), l.ctx, l.types, &l.anchors)) // named components; the entry Compile's run() calls first
	return l.out, append(diags, l.diags.List()...)
}

// lowerServiceSpec lowers components and the service layer of src.
func lowerServiceSpec(t *testing.T, src string) (*ir.Document, ir.Service, []ir.Diagnostic) {
	t.Helper()
	doc, diags := func() (*ir.Document, []ir.Diagnostic) {
		loadedDoc, loadDiags, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, loadOptions(Options{}.withDefaults()))
		require.NoError(t, err)
		require.NotNil(t, loadedDoc)
		l := newLowerer(loadedDoc, Options{}.withDefaults())
		l.diags.AppendAll(schema.LowerComponentSchemas(t.Context(), l.ctx, l.types, &l.anchors))
		auth, authDiags := auth.LowerSecuritySchemes(l.ctx)
		l.out.Auth = auth
		l.diags.AppendAll(authDiags)
		svc, tagDefs, svcDiags := operation.LowerService(t.Context(), l.ctx.WithAuth(l.out.Auth), l.types, &l.anchors, l.operationIDs)
		l.out.TagDefs = tagDefs
		l.diags.AppendAll(svcDiags)
		l.out.Services = []ir.Service{svc}
		return l.out, append(loadDiags, l.diags.List()...)
	}()
	require.Len(t, doc.Services, 1)
	return doc, doc.Services[0], diags
}

// lowerer is test scaffolding, not a compiler type. The compiler has no such
// struct: every lowering is a function of the context, the registry and the
// memos, and run owns the document being built (#177). Tests that drive one
// lowering in isolation still want those five things in one place, and holding
// them here keeps that convenience out of the production call graph.
type lowerer struct {
	ctx          lowering.Ctx
	out          *ir.Document
	types        *compile.Types
	diags        compile.Diags
	anchors      schema.AnchorIndex
	operationIDs map[string]string
}

// lowererOver is the only place the fixture's fields are initialised. Both
// entry points below build on it, so a field added to lowerer cannot reach one
// of them and miss the other — which is what newRawLowerer, hand-constructing
// the struct beside newLowerer, used to allow.
func lowererOver(ctx lowering.Ctx) *lowerer {
	types := compile.NewTypes(0)
	return &lowerer{
		ctx:          ctx,
		out:          &ir.Document{Types: types.Registry()},
		types:        types,
		operationIDs: make(map[string]string),
	}
}

// newLowerer builds the fixture over one loaded document, as run would. There
// is no srcIndex parameter: every test drives a single source, and the one the
// compiler varies lives in run's caller now.
func newLowerer(doc *load.Document, opts Options) *lowerer {
	return lowererOver(loweringCtx(doc, opts))
}

// newRawLowerer builds a lowerer over a hand-constructed document, bypassing the
// parser so nil slice/map entries (which the parser panics on) can be exercised.
func newRawLowerer(doc *soa.OpenAPI) *lowerer {
	return lowererOver(lowering.New(0, doc, ir.SourceInfo{}, "", lowering.Limits{}, overlay.Origin{}))
}

// componentID is the stable TypeID of a components-named schema, or of a
// sub-schema beneath one ("Holder/properties/inner").
//
// This one stays a per-package copy where the rest of the vocabulary moved to
// openapitest: internal/archtest's ID-grammar sweep permits a spelled-out ID in
// a test file and refuses one in a production file, and openapitest's files are
// production files. Restating the grammar independently of the compiler is what
// makes these lookups an oracle rather than a tautology, so deriving it through
// compile to satisfy the sweep would be the wrong way out.
func componentID(name string) ir.TypeID {
	return ir.TypeID("t/openapi/components/schemas/" + name)
}

// typeByName returns the named component schema's lowered TypeDef.
func typeByName(doc *ir.Document, name string) ir.TypeDef {
	return doc.Types[componentID(name)]
}

// assertHasErrorCode requires diags to carry an error-severity diagnostic with
// the given code.
func assertHasErrorCode(t *testing.T, diags []ir.Diagnostic, code string) {
	t.Helper()
	openapitest.AssertHasCode(t, diags, code, ir.SeverityError)
}
