package irverify

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

var preservedType = reflect.TypeOf(ir.Preserved(nil))

// checkPreserved asserts every Preserved entry carries the two things that make
// it reachable: a non-empty origin-namespaced key and a declared
// PreserveReason. Consumers select entries by reason — a validation emitter
// wants the §4.7 subset, a linter wants the degradations — so an entry left at
// the zero reason is bytes nothing can route, and one keyed "" cannot be looked
// up and is overwritten by the next empty-keyed entry. Both are compiler bugs
// of the same class checkRegistryKeys reports as ir/empty-*-id.
func checkPreserved(doc *ir.Document) []Violation {
	var vs []Violation
	walkValues(doc, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Map || v.Type() != preservedType {
			return true
		}
		iter := v.MapRange()
		for iter.Next() {
			vs = append(vs, preservedEntry(iter.Key().String(), iter.Value(), path)...)
		}
		return false // entries hold no references and no nested Preserved
	})
	return vs
}

// preservedEntry checks one entry of a Preserved map, reporting the key and the
// reason independently so a doubly-broken entry names both defects.
//
// Reason is read by field rather than through entry.Interface(), which panics
// on a value reached via an unexported field — the same guard checkNaming uses,
// keeping Verify an oracle that never crashes on a malformed document.
func preservedEntry(key string, entry reflect.Value, path string) []Violation {
	at := path + "[" + key + "]"
	var vs []Violation
	if key == "" {
		at = path + `[""]`
		vs = append(vs, Violation{
			Code:    "ir/empty-preserved-key",
			Message: "preserved map has an empty key",
			Path:    at,
		})
	}

	reason := ir.PreserveReason(entry.FieldByName("Reason").String())
	if reason == "" {
		return append(vs, Violation{
			Code:    "ir/empty-preserve-reason",
			Message: "preserved entry leaves its reason at the zero value",
			Path:    at,
		})
	}
	if !reason.Valid() {
		vs = append(vs, Violation{
			Code:    "ir/unknown-preserve-reason",
			Message: "preserved entry carries undeclared reason " + string(reason),
			Path:    at,
		})
	}
	return vs
}
