package irverify

import (
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// checkReferentialIntegrity asserts every typed-ID reference, map keys
// included, resolves in its registry, reporting each unresolved one as dangling.
// Most keys are an entry's own ID, but Service.Renames's are references.
//
// ir.IDReferences decides what is a reference and what it resolves against, as
// it does for pass.Validate, so the two report one defect alike. A class with no
// map on Document, such as ir.OpID, resolves against the identities the walk
// reads (GitHub #50), unless the walk truncated: a partial walk would report a
// reference to an unreached operation as dangling.
//
// ir.PropID stays out: pass.Validate's checkPropIDRefs holds it to a declared
// property.
func checkReferentialIntegrity(doc *ir.Document) walkCheck {
	refs := ir.NewIDReferences(doc)
	return walkCheck{
		visit: refs.Visit,
		report: func(decls declarations) []Violation {
			unresolved := refs.Unresolved(decls.truncated)
			vs := make([]Violation, 0, len(unresolved))
			for _, ref := range unresolved {
				vs = append(vs, Violation{
					Code:    "ir/dangling-" + ir.RefNoun(ref.Class) + "-ref",
					Message: "reference " + strconv.Quote(ref.ID) + " does not resolve in " + refs.Label(ref.Class),
					Path:    ref.Path,
				})
			}
			return vs
		},
	}
}
