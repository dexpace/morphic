package auth_test

import (
	"encoding/json/jsontext"
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// firstDiagAt returns the first diagnostic carrying code, so a test can assert
// on its provenance pointer.
func firstDiagAt(diags []ir.Diagnostic, code string) (ir.Diagnostic, bool) {
	for _, d := range diags {
		if d.Code == code {
			return d, true
		}
	}
	return ir.Diagnostic{}, false
}

// messagesAt returns the message of every diagnostic carrying code, in the order
// they were reported, so a caller can pin which names were reported and not only
// how many.
func messagesAt(diags []ir.Diagnostic, code string) []string {
	var out []string
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d.Message)
		}
	}
	return out
}

// messagesAtPointer returns the message of every diagnostic whose provenance
// names pointer, in report order. It filters on the pointer alone, so a report
// arriving at the right place under the wrong code is still returned.
func messagesAtPointer(diags []ir.Diagnostic, pointer string) []string {
	var out []string
	for _, d := range diags {
		if d.Provenance.Pointer == jsontext.Pointer(pointer) {
			out = append(out, d.Message)
		}
	}
	return out
}

// codesAtPointer returns the code of every diagnostic sited at pointer,
// sorted, so a caller pins which stage reported there, and how often: the
// loader's findings carry the pointer of the node they are about as well.
func codesAtPointer(diags []ir.Diagnostic, pointer string) []string {
	var out []string
	for _, d := range diags {
		if d.Provenance.Pointer == jsontext.Pointer(pointer) {
			out = append(out, d.Code)
		}
	}
	slices.Sort(out)
	return out
}

// sortedPointersAt returns every provenance pointer carried by a diagnostic
// with code, sorted so a caller pins the set rather than the walk's order.
func sortedPointersAt(diags []ir.Diagnostic, code string) []string {
	var out []string
	for _, d := range diags {
		if d.Code == code {
			out = append(out, string(d.Provenance.Pointer))
		}
	}
	slices.Sort(out)
	return out
}

// unmodeledKeys returns u's keys sorted, so a caller pins the whole set rather
// than probing for the ones it happened to think of.
func unmodeledKeys(u ir.Unmodeled) []string {
	return slices.Sorted(maps.Keys(u))
}
