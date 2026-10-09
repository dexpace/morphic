package irverify

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

// idFieldName is the field a node declares its own identity through. It is
// spelled here as well as in ir because this check reads the one declaration
// ir.DeclaredIDs drops — the empty one — and so cannot ask ir for the answer.
const idFieldName = "ID"

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
		if v.Kind() != reflect.Struct {
			return true
		}
		class, id, declares := declaredID(v)
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

// declaredID returns the identity v declares for itself: the class of ID, its
// value, and whether v declares one at all.
//
// It repeats the predicate ir.declaredID applies, a field named ID that v's own
// type declares, of a named string type, because that function answers only for
// non-empty IDs. The two must agree on what declares an identity. A promoted
// field is not a declaration; every type node promotes TypeCommon.ID, but such
// nodes are registry-keyed and skipped before it matters.
//
// No Document separates the two narrowing clauses, so only
// TestDeclaredID_ClassifiesEachShape pins them.
func declaredID(v reflect.Value) (class reflect.Type, id string, declares bool) {
	f, isDeclared := v.Type().FieldByName(idFieldName)
	if !isDeclared || len(f.Index) != 1 || !namedString(f.Type) {
		return nil, "", false
	}
	return f.Type, v.Field(f.Index[0]).String(), true
}

// namedString reports whether t is a named string type, the shape every ID class
// takes. A plain string is not one: it names something rather than identifying
// it.
func namedString(t reflect.Type) bool {
	return t.Kind() == reflect.String && t.PkgPath() != ""
}
