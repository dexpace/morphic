package irverify

import (
	"reflect"
	"strings"

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
		if n, ok := v.Interface().(ir.Naming); ok && isCased(n.Canonical) {
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

// isCased reports whether s still carries casing an emitter should own. The test
// is lowercase-idempotence, not unicode.IsUpper: a compiler neutralizes names
// with strings.ToLower, so a rune that has no lowercase form (double-struck ℤ,
// Mathematical Bold 𝐀, a Roman numeral) is already neutral even though IsUpper
// reports true for it. Only a canonical that ToLower would still change carries
// casing.
func isCased(s string) bool {
	return strings.ToLower(s) != s
}
