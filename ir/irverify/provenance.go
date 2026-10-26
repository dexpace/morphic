package irverify

import (
	"encoding/json/jsontext"
	"reflect"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

var provenanceType = reflect.TypeFor[ir.Provenance]()

// checkProvenance asserts every Provenance.Source addresses a declared entry of
// Document.Sources, and that the locators beside it are ones that source can
// have. Sources is a slice rather than an ID-keyed registry, so no derived
// registry resolves the index, and a stale one makes a report point at a file
// the document never loaded.
//
// It is document-wide rather than scoped to one carrier: the defect reads the
// same on a type, a diagnostic or an Unmodeled entry, and one walk reaches all
// of them.
func checkProvenance(doc *ir.Document) walkCheck {
	var vs []Violation
	declared := len(doc.Sources)
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Kind() != reflect.Struct || v.Type() != provenanceType {
				return true
			}
			src := int(v.FieldByName("Source").Int())
			if sourceOutOfRange(src, declared) {
				vs = append(vs, Violation{
					Code: "ir/provenance-source-out-of-range",
					Message: "provenance source " + strconv.Itoa(src) + " does not address one of " +
						strconv.Itoa(declared) + " declared sources",
					Path: at.String(),
				})
			}
			vs = appendLocatorViolations(vs, v, src, at)
			return false // Provenance holds no nested Provenance
		},
		report: reporting(&vs),
	}
}

// appendLocatorViolations reports the locators prov cannot hold: a pointer that
// is not RFC 6901, a position that is not 1-based, and either one on
// ir.NoSource, where there is no file for it to be inside.
//
// The pointer is checked with jsontext.Pointer.IsValid, which refuses a '~'
// that escapes nothing — a form a consumer walking the pointer with that type's
// methods would misread. IsValid refuses ill-formed UTF-8 too, but those bytes
// are checkUTF8's to report, under the one code every string answers to, so the
// grammar is read with them replaced rather than reported a second time.
func appendLocatorViolations(vs []Violation, prov reflect.Value, src int, at ir.WalkPath) []Violation {
	pointer := prov.FieldByName("Pointer").String()
	position := prov.FieldByName("Position")
	line := position.FieldByName("Line").Int()
	column := position.FieldByName("Column").Int()
	positioned := line != 0 || column != 0

	if !jsontext.Pointer(strings.ToValidUTF8(pointer, "\uFFFD")).IsValid() {
		vs = append(vs, Violation{
			Code:    "ir/provenance-pointer-malformed",
			Message: "provenance pointer " + strconv.Quote(pointer) + " is not an RFC 6901 JSON Pointer",
			Path:    at.String(),
		})
	}
	if positioned && (line < 1 || column < 0) {
		vs = append(vs, Violation{
			Code: "ir/provenance-position-malformed",
			Message: "provenance position " + strconv.FormatInt(line, 10) + ":" +
				strconv.FormatInt(column, 10) + " is not a 1-based line with a column of 0 or more",
			Path: at.String(),
		})
	}
	if src == ir.NoSource && (pointer != "" || positioned) {
		vs = append(vs, Violation{
			Code:    "ir/provenance-locator-without-source",
			Message: "provenance locates a construct inside a source but names none",
			Path:    at.String(),
		})
	}
	return vs
}

// sourceOutOfRange reports whether index fails to address one of declared
// sources.
//
// ir.NoSource is in range everywhere: it is the declared way to say a node
// addresses no input file, as every diagnostic an IR pass emits about the
// document itself does. Holding those diagnostics to the source table would
// make a document less valid the more spec problems the validator found. No
// other negative index is admitted: a second, undeclared sentinel is the drift
// this check exists to catch.
//
// With no sources declared only the zero value is admitted; ir/sources-absent
// reports the empty table once, not per node.
func sourceOutOfRange(index, declared int) bool {
	if declared == 0 {
		return index != 0 && index != ir.NoSource
	}
	return !ir.Provenance{Source: index}.InTable(declared)
}
