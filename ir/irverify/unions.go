package irverify

import (
	"github.com/dexpace/morphic/ir"
)

// checkUnions asserts every union in the type registry declares at least one
// variant (ir-design §4.4). A union is the choice between its variants, so a
// union of none is a type no value inhabits, and it is not a shape any source
// format can express: `oneOf: []` is refused before it lowers, and every other
// format's sum requires at least one member. A union that reaches the IR with
// none was therefore built by a lowering that dropped every variant it meant to
// add — our bug, which is what makes it a Violation and not an ir.Diagnostic.
//
// Downstream it is worse than the missing variants are on their own: an emitter
// switching over a union's variants renders a type with no arms and no error, so
// the loss surfaces as generated code that compiles and can never be
// constructed.
//
// Two neighbouring shapes are deliberately not reported here, because the
// compiler produces both from documents the specification allows — a Violation
// claims a compiler defect, so neither belongs in this channel:
//
//   - A union of exactly one variant. `oneOf: [{$ref: X}]` lowers to one, and
//     invariant #2 forbids a compiler collapsing it. It is not a choice, but it
//     is inhabited and it is a faithful lowering.
//   - Two variants naming one target. `oneOf: [{$ref: X}, {$ref: X}]` lowers to
//     exactly that. It is degenerate rather than impossible, so if it is worth
//     reporting at all it is a spec-author problem for pass.Validate.
//
// Unions live only in the type registry — invariant #3 keeps every named entity
// there and lets no node embed another — so iterating it reaches every one and
// this check needs no walk of its own.
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
