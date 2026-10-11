package irverify

import (
	"github.com/dexpace/morphic/ir"
)

// checkUnions asserts every union in the type registry declares at least one
// variant (ir-design §4.4). A union of none is a type no value inhabits and no
// source format can express, so one in the IR came from a lowering that dropped
// every variant it meant to add: a Violation, not an ir.Diagnostic.
//
// Two shapes are not reported, since the compiler produces both from valid
// documents:
//
//   - One variant: `oneOf: [{$ref: X}]` lowers to it, and invariant #2 forbids
//     collapsing it.
//   - Two variants naming one target: degenerate rather than impossible, a
//     pass.Validate matter at most.
func checkUnions(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		if ir.IsNilTypeDef(td) {
			continue // checkRegistryKeys reports the nil entry itself
		}
		u, isUnion := td.(*ir.Union)
		if !isUnion || len(u.Variants) > 0 {
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/union-no-variants",
			Message: "union declares no variants, so no value inhabits it",
			Path:    "types[" + string(id) + "]",
		})
	}
	return vs
}
