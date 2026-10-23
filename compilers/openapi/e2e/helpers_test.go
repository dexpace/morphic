package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

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

// componentID is the stable TypeID of a components-named schema, or of a
// sub-schema beneath one ("Holder/properties/inner"). It stays a test-file
// copy for the reason openapitest's doc comment gives.
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
