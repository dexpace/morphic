// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/operation"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/compilers/openapi/internal/servers"
	"github.com/dexpace/morphic/ir"
)

// This half of the suite drives whole documents through the compiler. The
// operation walk is what turns a path item into operations, parameters and
// content, and the positions it hoists only exist once it has run — so the
// fixtures are specs, not hand-built values, and reaching the compiler needs an
// external test package.

// parseFull runs the whole public compiler pipeline over src. That reach back
// through openapi is also why openapitest cannot hold it — see that package's
// doc comment.
func parseFull(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{openapitest.SourceOf(src)}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// compileService compiles src and returns the document, its single service,
// and every diagnostic.
func compileService(t *testing.T, src string) (*ir.Document, ir.Service, []ir.Diagnostic) {
	t.Helper()
	doc, diags := parseFull(t, src)
	require.Len(t, doc.Services, 1)
	return doc, doc.Services[0], diags
}

// typeByName returns the named component schema's lowered TypeDef. It stays a
// per-package copy for the reason openapitest's doc comment gives: a
// spelled-out ID belongs in a test file, which the idderivations rule
// does not hold.
func typeByName(doc *ir.Document, name string) ir.TypeDef {
	return doc.Types[ir.TypeID("t/openapi/components/schemas/"+name)]
}

// serviceWithGrouping runs the phases beneath the service walk and then the walk
// itself under a chosen grouping strategy. It names the strategy directly rather
// than routing one through the compiler's public options, because grouping is
// the only thing these tests vary and the projection in between is the
// compiler's business, not theirs.
func serviceWithGrouping(t *testing.T, src string, grouping lowering.GroupingStrategy) (ir.Service, []ir.Diagnostic) {
	t.Helper()
	var acc compile.Diags
	svc, loadDiags := walkService(t, src, grouping, acc.AppendAll)
	return svc, append(loadDiags, acc.List()...)
}

// serviceAsReported is serviceWithGrouping under GroupByTags, returning every
// diagnostic after the load's as each phase reported it, before compile.Diags
// drops a repeat: what a test needs to hold a lowering to reporting a finding
// once rather than leave that to the dedupe.
func serviceAsReported(t *testing.T, src string) (ir.Service, []ir.Diagnostic) {
	t.Helper()
	var reported []ir.Diagnostic
	svc, _ := walkService(t, src, lowering.GroupByTags, func(diags []ir.Diagnostic) {
		reported = append(reported, diags...)
	})
	return svc, reported
}

// walkService loads src and runs the phases beneath the service walk, then the
// walk itself under grouping, handing report each phase's diagnostics in turn.
// It returns the service and the load's diagnostics.
func walkService(t *testing.T, src string, grouping lowering.GroupingStrategy,
	report func([]ir.Diagnostic),
) (ir.Service, []ir.Diagnostic) {
	t.Helper()
	loadedDoc, loadDiags, err := load.Load(t.Context(), 0, openapitest.SourceOf(src), load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)

	types := compile.NewTypes()
	c := lowering.New(0, loadedDoc.Doc, loadedDoc.Source, grouping, lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(loadedDoc)
	report(schema.LowerComponentSchemas(t.Context(), c, types))

	schemes, schemeDiags := auth.LowerSecuritySchemes(c)
	report(schemeDiags)
	svc, _, svcDiags := operation.LowerService(t.Context(), c, types, servers.NewRegistry(), schemes)
	report(svcDiags)
	_, finished := types.Finish(t.Context())
	report(finished)
	return svc, loadDiags
}

// errorFindings spells each error in diags as its code and pointer, in the order
// reported.
func errorFindings(diags []ir.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == ir.SeverityError {
			out = append(out, d.Code+" at "+string(d.Provenance.Pointer))
		}
	}
	return out
}

const multipartVariantsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /noschema:
    post:
      operationId: noSchema
      requestBody:
        content:
          multipart/form-data: {}
      responses: {"200": {description: ok}}
  /noprops:
    post:
      operationId: noProps
      requestBody:
        content:
          multipart/form-data:
            schema: {type: object}
      responses: {"200": {description: ok}}
  /plainprops:
    post:
      operationId: plainProps
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties: {a: {type: string}, b: {type: integer}}
      responses: {"200": {description: ok}}
  /examples:
    get:
      operationId: exGet
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: string}
              examples:
                empty: {summary: no value here}
`
