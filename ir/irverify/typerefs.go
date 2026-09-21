package irverify

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

var typeRefType = reflect.TypeFor[ir.TypeRef]()

// checkTypeRefs asserts every ir.TypeRef in the document names a target. A
// position that admits no type says so with a nil *TypeRef (Model.Base,
// Content.Item), never with a Target of "" — Target carries no omitempty, and no
// position documents an empty one as meaning anything. So an empty one, whether
// by value, in a slice, or behind a pointer that was allocated and given nothing
// to name, was left by a lowering that dropped what it meant to add: our bug,
// hence a Violation. Downstream it is a union arm, property or element no emitter
// can render. The union case is the one with a reproducer (GitHub #397): one
// variant satisfies checkUnions, and checkReferentialIntegrity never sees an
// empty target.
//
// The rule is keyed by the type rather than by position, so a new position is
// held to it the moment it exists. It is not folded into collectRefs, whose
// empty-ID skip is right for the bare ID fields it also reaches (see there).
//
// The path names the Target field, where checkReferentialIntegrity reports a
// dangling one, so two claims about one reference land at one place.
func checkTypeRefs(doc *ir.Document, _ declarations) ([]Violation, bool) {
	var vs []Violation
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Struct || v.Type() != typeRefType {
			return true
		}
		if v.FieldByName("Target").String() == "" {
			vs = append(vs, Violation{
				Code:    "ir/type-ref-no-target",
				Message: "type reference names no target",
				Path:    path + ".Target",
			})
		}
		return false // a TypeRef holds nothing else to descend into
	})
	return vs, truncated
}
