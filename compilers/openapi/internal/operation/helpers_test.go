package operation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/operation"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
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

// lowerServiceSpec compiles src and returns the document, its single service,
// and every diagnostic.
func lowerServiceSpec(t *testing.T, src string) (*ir.Document, ir.Service, []ir.Diagnostic) {
	t.Helper()
	doc, diags := parseFull(t, src)
	require.Len(t, doc.Services, 1)
	return doc, doc.Services[0], diags
}

// typeByName returns the named component schema's lowered TypeDef. It stays a
// per-package copy for the reason openapitest's doc comment gives: a
// spelled-out ID belongs in a test file, where internal/archtest's ID-grammar
// sweep permits it.
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
	loadedDoc, loadDiags, err := load.Load(t.Context(), 0, openapitest.SourceOf(src), load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)

	types := compile.NewTypes(0)
	c := lowering.New(0, loadedDoc.Doc, loadedDoc.Source, grouping, lowering.Limits{}, lowering.StreamingMedia{}, overlay.Origin{})
	var anchors schema.AnchorIndex
	var acc compile.Diags
	acc.AppendAll(schema.LowerComponentSchemas(t.Context(), c, types, &anchors))

	svc, _, svcDiags := operation.LowerService(t.Context(), c, types, &anchors, make(map[string]string))
	acc.AppendAll(svcDiags)
	return svc, append(loadDiags, acc.List()...)
}
