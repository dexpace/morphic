package operation

import (
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
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

// loweredFor loads src and returns the fixture over it with nothing lowered
// yet, plus the load diagnostics.
func loweredFor(t *testing.T, src string) (*lowerer, []ir.Diagnostic) {
	t.Helper()
	loadedDoc, diags, err := load.Load(t.Context(), 0,
		compilers.Source{Path: "spec.yaml", Data: []byte(src)}, load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc, "load returned no document: %+v", diags)
	types := compile.NewTypes(0)
	return &lowerer{
		ctx:          lowering.New(0, loadedDoc.Doc, loadedDoc.Source, lowering.GroupByTags, lowering.ExtensionPromotions{}, overlay.Origin{}),
		out:          &ir.Document{Types: types.Registry()},
		types:        types,
		operationIDs: make(map[string]string),
	}, diags
}

// newRawLowerer builds a fixture over a hand-constructed document, bypassing
// the parser so nil slice/map entries can be exercised.
func newRawLowerer(doc *soa.OpenAPI) *lowerer {
	types := compile.NewTypes(0)
	return &lowerer{
		ctx:          lowering.New(0, doc, ir.SourceInfo{}, "", lowering.ExtensionPromotions{}, overlay.Origin{}),
		out:          &ir.Document{Types: types.Registry()},
		types:        types,
		operationIDs: make(map[string]string),
	}
}

// componentSpec wraps a components/schemas block in a minimal 3.1 document.
func componentSpec(schemas string) string {
	return "openapi: 3.1.0\n" +
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

// requireNoErrorDiags fails the test if any diagnostic has error severity.
func requireNoErrorDiags(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	d, ok := ir.FirstError(diags)
	require.False(t, ok, "unexpected error diagnostic: %+v", d)
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

// emptyEitherSchema is a JSONSchema whose either-value has neither a Left schema
// nor a Right bool set: IsSchema() is true yet GetSchema() is nil. The parser
// never produces this, so it drives the nil-schema guards.
func emptyEitherSchema() *oas3.JSONSchema[oas3.Referenceable] {
	return oas3.NewJSONSchemaFromSchema[oas3.Referenceable](nil)
}

// assertHasCode requires diags to carry a diagnostic with the given code at the
// given severity.
func assertHasCode(t *testing.T, diags []ir.Diagnostic, code string, sev ir.Severity) {
	t.Helper()
	for _, d := range diags {
		if d.Code == code && d.Severity == sev {
			return
		}
	}
	t.Fatalf("expected a %v diagnostic with code %q, got %+v", sev, code, diags)
}

// countDiagsAt counts the diagnostics matching code and sev exactly.
func countDiagsAt(diags []ir.Diagnostic, code string, sev ir.Severity) int {
	var n int
	for _, d := range diags {
		if d.Code == code && d.Severity == sev {
			n++
		}
	}
	return n
}

// indexBy builds a lookup keyed by key(item).
func indexBy[T any, K comparable](items []T, key func(T) K) map[K]T {
	out := make(map[K]T, len(items))
	for _, item := range items {
		out[key(item)] = item
	}
	return out
}
