package irverify_test

import (
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/ir/irverify"
)

// verifyDeclared runs Verify on a copy of doc that declares the namespaces its
// IDs live in, unless doc declares some itself.
//
// A document is stamped with an irVersion by the fixture that builds it; this
// does the same for Document.IDSpaces, so a test about some other rule is not
// also a test of the declaration. Tests of the declaration build their own and
// call irverify.Verify directly.
func verifyDeclared(doc *ir.Document) []irverify.Violation {
	if doc == nil || doc.IDSpaces != nil {
		return irverify.Verify(doc)
	}
	declared := *doc
	declared.IDSpaces = irtest.SpacesUsed(doc)
	return irverify.Verify(&declared)
}
