package openapi

import (
	"testing"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// componentID is the stable TypeID of a components-named schema, or of a
// sub-schema beneath one ("Holder/properties/inner").
//
// This one stays a per-package copy where the rest of the vocabulary moved to
// openapitest: the idderivations rule permits a spelled-out ID in a test
// file and refuses one in a production file, and openapitest's files are
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
