package irverify

import (
	"reflect"
	"unicode"

	"github.com/dexpace/morphic/ir"
)

var namingType = reflect.TypeOf(ir.Naming{})

// checkNaming asserts no Naming.Canonical carries casing: the IR stores neutral
// lower_snake words and leaves all casing to emitters (invariant #4). It reuses
// walkValues to reach every ir.Naming value in the document.
func checkNaming(doc *ir.Document) []Violation {
	var vs []Violation
	walkValues(doc, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Struct || v.Type() != namingType {
			return true
		}
		if n, ok := v.Interface().(ir.Naming); ok && hasUpper(n.Canonical) {
			vs = append(vs, Violation{
				Code:    "ir/naming-cased",
				Message: "canonical name " + n.Canonical + " carries casing; store neutral words",
				Path:    path,
			})
		}
		return false // Naming holds no references or nested Naming to descend into
	})
	return vs
}

// hasUpper reports whether s contains an uppercase letter.
func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}
