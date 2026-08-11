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

// checkBigVals asserts every numeric literal the document carries is what
// ir.BigVal promises: a decimal literal that reads back as a JSON number, in the
// canonical form ir.NewBigVal produces.
//
// This is the same hazard checkRawPayloads covers for the other verbatim-text
// carrier, and it has the same shape: the text is copied wherever the value
// goes, and the grammar it must satisfy is enforced only at construction.
// ir.BigVal is a defined string type, so ir.BigVal(raw) compiles and skips
// ir.NewBigVal entirely, and — unlike the sealed TypeDef sum — it carries no
// UnmarshalJSON, so a document decoded from JSON never meets the constructor at
// all (GitHub #282). Round-tripping is not the safety net here: the value is
// carried faithfully precisely because it is a string.
//
// The two codes are separate because they name different repairs. A value the
// constructor rejects is not a number at all; a value it accepts but rewrites is
// a number spelled a way JSON does not admit — a leading "+", a redundant
// leading zero, a bare leading dot — which a consumer splicing the text into
// generated source or into JSON emits as invalid output.
//
// Reaching the literals through the walk rather than a list of carriers is what
// makes this complete: Constraints.Min, Max and MultipleOf and Value.Num are the
// fields today, and a numeric field added to the IR is covered the moment it
// exists.
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
