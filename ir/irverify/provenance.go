package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var provenanceType = reflect.TypeOf(ir.Provenance{})

// checkProvenance asserts every Provenance.Source addresses a declared entry of
// Document.Sources. The index is a reference like any typed ID — a stale or
// off-by-one one makes a report point at a file the document never loaded —
// but Sources is a slice rather than an ID-keyed registry, so it cannot ride
// refKindByType and gets its own check.
//
// It is document-wide rather than scoped to any one carrier: the defect reads
// the same on a type, a diagnostic, or a Preserved entry, and walkValues
// reaches all of them for one traversal.
func checkProvenance(doc *ir.Document) []Violation {
	var vs []Violation
	declared := len(doc.Sources)
	walkValues(doc, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Struct || v.Type() != provenanceType {
			return true
		}
		// Read Source by field: a Provenance reached through an unexported field
		// is read-only, and Interface() panics on it where FieldByName does not.
		src := int(v.FieldByName("Source").Int())
		if sourceOutOfRange(src, declared) {
			vs = append(vs, Violation{
				Code: "ir/provenance-source-out-of-range",
				Message: "provenance source " + strconv.Itoa(src) + " does not address one of " +
					strconv.Itoa(declared) + " declared sources",
				Path: path,
			})
		}
		return false // Provenance holds no references and no nested Provenance
	})
	return vs
}

// sourceOutOfRange reports whether index fails to address one of declared
// sources.
//
// A document declaring none is held only to the zero value: it makes no claim
// about source indexing, and hand-built fixtures are that shape. Compiler
// output always stamps its one loaded source, so the tolerance costs nothing on
// the population this check exists for.
func sourceOutOfRange(index, declared int) bool {
	if declared == 0 {
		return index != 0
	}
	return index < 0 || index >= declared
}
