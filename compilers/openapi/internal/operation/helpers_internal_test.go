package operation

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// The tests in this half read an unexported lowering directly. Everything
// reachable through LowerService lives in package operation_test, which can
// import the compiler and drive a spec end to end; nothing importable from
// there would reach these.

// lowerer is test scaffolding, not a compiler type: every lowering is a
// function of the context, the registry and the memos (#177), and this holds
// the ones a single-lowering test wants in one place.
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
// of them and miss the other.
func lowererOver(ctx lowering.Ctx) *lowerer {
	types := compile.NewTypes(0)
	return &lowerer{
		ctx:          ctx,
		out:          &ir.Document{Types: types.Registry()},
		types:        types,
		operationIDs: make(map[string]string),
	}
}

// loweredFor loads src and returns the fixture over it with nothing lowered
// yet, plus the load diagnostics.
func loweredFor(t *testing.T, src string) (*lowerer, []ir.Diagnostic) {
	t.Helper()
	loadedDoc, diags, err := load.Load(t.Context(), 0, openapitest.SourceOf(src), load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc, "load returned no document: %+v", diags)
	return lowererOver(lowering.New(0, loadedDoc.Doc, loadedDoc.Source,
		lowering.GroupByTags, overlay.Origin{})), diags
}

// newRawLowerer builds a fixture over a hand-constructed document, bypassing
// the parser so nil slice/map entries can be exercised.
func newRawLowerer(doc *soa.OpenAPI) *lowerer {
	return lowererOver(lowering.New(0, doc, ir.SourceInfo{}, "", overlay.Origin{}))
}

// lowerServiceSpec loads src and runs the phases the service walk needs beneath
// it — component schemas, then security schemes — before the walk itself. It
// drives LowerService directly rather than the compiler, which is what lets a
// test in this package hold an unexported lowering to what the walk did with it.
//
// It returns the service rather than the document being assembled: nothing here
// asks what the compiler would have built around it.
func lowerServiceSpec(t *testing.T, src string) (ir.Service, []ir.Diagnostic) {
	t.Helper()
	l, loadDiags := loweredFor(t, src)
	l.diags.AppendAll(schema.LowerComponentSchemas(l.ctx, l.types, &l.anchors))
	schemes, authDiags := auth.LowerSecuritySchemes(l.ctx)
	l.out.Auth = schemes
	l.diags.AppendAll(authDiags)

	svc, _, svcDiags := LowerService(l.ctx.WithAuth(schemes), l.types, &l.anchors, l.operationIDs)
	l.diags.AppendAll(svcDiags)
	return svc, append(loadDiags, l.diags.List()...)
}
