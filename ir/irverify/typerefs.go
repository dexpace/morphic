package irverify

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

var typeRefType = reflect.TypeFor[ir.TypeRef]()

// checkTypeRefs asserts every ir.TypeRef names a target, and is Nullable where
// it is marked Inferred. A position admitting no type uses a nil *TypeRef, so
// an empty Target is an addition a lowering dropped, which no other check sees
// (GitHub #397). The marker names the heuristic that read null in, so one on a
// reference admitting none outlived the bit it qualified.
//
// The check is keyed by type, so a new position is held to it; collectRefs
// skips empty IDs, rightly for bare ID fields. The path names the field at
// fault.
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
		if inferred := v.FieldByName("Inferred").String(); inferred != "" && !v.FieldByName("Nullable").Bool() {
			vs = append(vs, Violation{
				Code:    "ir/type-ref-inferred-not-nullable",
				Message: "type reference is marked inferred by " + inferred + " but admits no null",
				Path:    path + ".Inferred",
			})
		}
		return false // a TypeRef holds nothing else to descend into
	})
	return vs, truncated
}
