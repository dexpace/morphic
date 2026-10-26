package irverify

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

// checkDeclaredIDs asserts every node that declares an identity of its own
// carries one, and a well-formed one. An ID derives from the source pointer, so
// an empty or malformed one is a compiler bug; an empty one surfaces as a
// dangling reference at the referring site, not as the missing declaration.
//
// Nothing else reaches these nodes. The registry checks read a type, scheme,
// channel or message by its key, but an Operation, OperationGroup, Service or
// Property has none, and ir.IDDeclarations drops an empty ID before
// checkDuplicateIDs sees it (GitHub #289). Classes with a registry are skipped.
func checkDeclaredIDs(*ir.Document) walkCheck {
	keyed := ir.KeyedClasses()
	var ids ir.IDReader
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			class, id, declares := ids.DeclaredIDOf(v)
			if !declares {
				return true
			}
			if keyed[class] {
				return true // the registry key is where these classes are read
			}
			vs = appendDeclaredIDViolations(vs, v, class, id, at)
			return true
		},
		report: reporting(&vs),
	}
}
