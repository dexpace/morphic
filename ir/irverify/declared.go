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
// Property has none, and ir.DeclaredIDs drops an empty ID before
// checkDuplicateIDs sees it (GitHub #289). Classes with a registry are skipped.
func checkDeclaredIDs(doc *ir.Document, _ declarations) ([]Violation, bool) {
	keyed := ir.DocumentRegistries(doc)
	var vs []Violation
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		class, id, declares := ir.DeclaredIDOf(v)
		if !declares {
			return true
		}
		if _, hasRegistry := keyed[class]; hasRegistry {
			return true // the registry key is where these classes are read
		}
		vs = appendDeclaredIDViolations(vs, v, class, id, path)
		return true
	})
	return vs, truncated
}
