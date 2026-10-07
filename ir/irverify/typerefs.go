package irverify

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

var typeRefType = reflect.TypeFor[ir.TypeRef]()

// checkTypeRefs asserts every ir.TypeRef names a target. A position admitting
// no type uses a nil *TypeRef (Model.Base, Content.Item), never an empty
// Target, so an empty one was left by a lowering that dropped what it meant to
// add: our bug. Downstream it is a union arm, property or element no emitter
// can render, and neither checkUnions nor checkReferentialIntegrity sees it
// (GitHub #397).
//
// The check is keyed by type, so a new position is held to it. It is not folded
// into collectRefs, whose empty-ID skip is right for bare ID fields. The path
// names Target, as checkReferentialIntegrity's report does.
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
