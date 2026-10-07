package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var (
	bigValType    = reflect.TypeFor[ir.BigVal]()
	bigValPtrType = reflect.TypeFor[*ir.BigVal]()
)

// checkBigVals asserts every numeric literal reads back as a JSON number, in
// the canonical form ir.NewBigVal produces.
//
// ir.BigVal is a defined string type, so ir.BigVal(raw) skips ir.NewBigVal, and
// with no UnmarshalJSONFrom a decoded document never meets the constructor
// (GitHub #282). A round trip cannot catch it: a string is carried faithfully.
//
// The two codes name different repairs: a value the constructor rejects is not
// a number; one it rewrites is spelled a way JSON does not admit (a leading
// "+", a redundant zero, a bare dot). The walk reaches the literals, so a new
// numeric field is covered at once.
func checkBigVals(doc *ir.Document, _ declarations) ([]Violation, bool) {
	var vs []Violation
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		switch v.Type() {
		case bigValPtrType:
			// A bound carried by pointer is present because something set it, so
			// an empty literal there is a defect rather than an absence. Reading
			// it here rather than descending is what tells the two apart.
			if !v.IsNil() {
				vs = appendBigVal(vs, v.Elem().String(), path)
			}
			return false
		case bigValType:
			// Value.Num is not a pointer and is the zero string on every Value
			// that is not a number — most of the values a document holds — so an
			// empty one here says the field is unused, not that it is broken.
			// That reading holds only beside a non-number Value: an empty Num on
			// one whose Kind is number is checkValues' to report
			// (ir/value-missing-payload), not this check's.
			if literal := v.String(); literal != "" {
				vs = appendBigVal(vs, literal, path)
			}
			return false
		default:
			return true
		}
	})
	return vs, truncated
}

// appendBigVal reports the two ways one literal can break ir.BigVal's contract.
// The literal is quoted because the values worth reporting are the ones that do
// not look like numbers, the empty string among them.
func appendBigVal(vs []Violation, literal, path string) []Violation {
	canonical, err := ir.NewBigVal(literal)
	if err != nil {
		return append(vs, Violation{
			Code: "ir/bigval-not-numeric",
			Message: "numeric value " + strconv.Quote(literal) +
				" is not a decimal literal, so it does not read back as a JSON number",
			Path: path,
		})
	}
	if string(canonical) == literal {
		return vs
	}
	return append(vs, Violation{
		Code: "ir/bigval-not-canonical",
		Message: "numeric value " + strconv.Quote(literal) + " is not the JSON form " +
			strconv.Quote(string(canonical)) + " ir.NewBigVal produces for it",
		Path: path,
	})
}
