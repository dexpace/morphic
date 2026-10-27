package irverify

import (
	"github.com/dexpace/morphic/ir"
)

// checkUnions asserts every union in the type registry has a variant and is
// in a tag mode ir-design §4.4 defines. A union of none is a type no value
// inhabits, so a lowering dropped every variant it meant to add; one carrying a
// Discriminator without WireTagged reads under §4.4 as untagged, so a consumer
// ignores its tag. Both are a lowering's defects: a Violation, not an
// ir.Diagnostic.
//
// Two shapes pass, since valid documents produce them: one variant, which
// `oneOf: [{$ref: X}]` lowers to and invariant #2 keeps; and two variants
// naming one target, degenerate rather than impossible.
func checkUnions(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		if ir.IsNilTypeDef(td) {
			continue // checkRegistryKeys reports the nil entry itself
		}
		u, isUnion := td.(*ir.Union)
		if !isUnion {
			continue
		}
		if len(u.Variants) == 0 {
			vs = append(vs, Violation{
				Code:    "ir/union-no-variants",
				Message: "union declares no variants, so no value inhabits it",
				Path:    typePath(id),
			})
		}
		if u.Discriminator != nil && !u.WireTagged {
			vs = append(vs, Violation{
				Code:    "ir/union-discriminator-untagged",
				Message: "union carries a discriminator but is not WireTagged, a tag mode ir-design §4.4 does not define",
				Path:    typePath(id),
			})
		}
	}
	return vs
}
