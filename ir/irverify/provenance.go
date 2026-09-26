package irverify

import (
	"encoding/json/jsontext"
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var provenanceType = reflect.TypeFor[ir.Provenance]()

// checkProvenance asserts every Provenance.Source addresses a declared entry of
// Document.Sources, and that the locators beside it are ones that source can
// have. The index is a reference like any typed ID — a stale or off-by-one one
// makes a report point at a file the document never loaded — but Sources is a
// slice rather than an ID-keyed registry, so no derived registry resolves it
// and it gets its own check.
//
// It is document-wide rather than scoped to any one carrier: the defect reads
// the same on a type, a diagnostic, or an Unmodeled entry, and one walk reaches
// all of them. The bool reports whether that walk was cut short; Verify folds it
// into the document's one ir/walk-truncated violation.
func checkProvenance(doc *ir.Document, _ declarations) ([]Violation, bool) {
	var vs []Violation
	declared := len(doc.Sources)
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Struct || v.Type() != provenanceType {
			return true
		}
		src := int(v.FieldByName("Source").Int())
		if sourceOutOfRange(src, declared) {
			vs = append(vs, Violation{
				Code: "ir/provenance-source-out-of-range",
				Message: "provenance source " + strconv.Itoa(src) + " does not address one of " +
					strconv.Itoa(declared) + " declared sources",
				Path: path,
			})
		}
		vs = appendLocatorViolations(vs, v, src, path)
		return false // Provenance holds no references and no nested Provenance
	})
	return vs, truncated
}

// appendLocatorViolations reports the locators prov cannot hold: a pointer that
// is not RFC 6901, a position that is not 1-based, and either one on
// ir.NoSource, where there is no file for it to be inside.
//
// The pointer is checked with jsontext.Pointer.IsValid, which also refuses a
// '~' that escapes nothing and bytes that are not UTF-8 — the forms a consumer
// walking the pointer with that type's methods would misread.
func appendLocatorViolations(vs []Violation, prov reflect.Value, src int, path string) []Violation {
	pointer := jsontext.Pointer(prov.FieldByName("Pointer").String())
	position := prov.FieldByName("Position")
	line := position.FieldByName("Line").Int()
	column := position.FieldByName("Column").Int()
	positioned := line != 0 || column != 0

	if !pointer.IsValid() {
		vs = append(vs, Violation{
			Code:    "ir/provenance-pointer-malformed",
			Message: "provenance pointer " + strconv.Quote(string(pointer)) + " is not an RFC 6901 JSON Pointer",
			Path:    path,
		})
	}
	if positioned && (line < 1 || column < 0) {
		vs = append(vs, Violation{
			Code: "ir/provenance-position-malformed",
			Message: "provenance position " + strconv.FormatInt(line, 10) + ":" +
				strconv.FormatInt(column, 10) + " is not a 1-based line with a column of 0 or more",
			Path: path,
		})
	}
	if src == ir.NoSource && (pointer != "" || positioned) {
		vs = append(vs, Violation{
			Code:    "ir/provenance-locator-without-source",
			Message: "provenance locates a construct inside a source but names none",
			Path:    path,
		})
	}
	return vs
}

// sourceOutOfRange reports whether index fails to address one of declared
// sources.
//
// ir.NoSource is in range everywhere: it is the declared way to say the node
// came from no input file, which is what every diagnostic an IR pass emits about
// the document itself carries, and engine.Run folds those into
// Document.Diagnostics. Holding them to the source table would make a document
// less valid the more spec problems the validator found in it. No other negative
// index is admitted — a second, undeclared sentinel is exactly the drift this
// check exists to catch.
//
// A document declaring no sources is held only to the zero value: it makes no
// claim about source indexing, and hand-built fixtures are that shape. Compiler
// output always stamps its one loaded source, so the tolerance costs nothing on
// the population this check exists for.
func sourceOutOfRange(index, declared int) bool {
	if index == ir.NoSource {
		return false
	}
	if declared == 0 {
		return index != 0
	}
	return index < 0 || index >= declared
}
