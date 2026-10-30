// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var typeRefType = reflect.TypeFor[ir.TypeRef]()

// checkTypeRefs asserts every ir.TypeRef names a target, and is Nullable where
// it is marked Inferred. A position admitting no type uses a nil *TypeRef, so
// an empty Target is an addition a lowering dropped, which no other check sees
// (GitHub #397). The marker names the heuristic that read null in, so one on a
// reference admitting none outlived the bit it qualified.
//
// The check is keyed by type, so a new position is held to it;
// checkReferentialIntegrity skips empty IDs, rightly for bare ID fields. The
// path names the field at fault.
func checkTypeRefs(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Kind() != reflect.Struct || v.Type() != typeRefType {
				return true
			}
			if v.FieldByName("Target").String() == "" {
				vs = append(vs, Violation{
					Code:    "ir/type-ref-no-target",
					Message: "type reference names no target",
					Path:    at.String() + ".Target",
				})
			}
			if inferred := v.FieldByName("Inferred").String(); inferred != "" && !v.FieldByName("Nullable").Bool() {
				vs = append(vs, Violation{
					Code:    "ir/type-ref-inferred-not-nullable",
					Message: "type reference is marked inferred by " + strconv.Quote(inferred) + " but admits no null",
					Path:    at.String() + ".Inferred",
				})
			}
			return false // a TypeRef holds nothing else this check looks for
		},
		report: reporting(&vs),
	}
}
