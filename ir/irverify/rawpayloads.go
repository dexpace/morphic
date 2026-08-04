package irverify

import (
	"encoding/json"
	"reflect"

	"github.com/dexpace/morphic/ir"
)

var (
	unmodeledType = reflect.TypeFor[ir.Unmodeled]()
	rawConfigType = reflect.TypeFor[ir.RawConfig]()
)

// checkRawPayloads asserts the two maps that carry source JSON verbatim hold
// something a document can be marshaled with, and that each Unmodeled entry is
// one a consumer can route.
//
// Both maps are checked in one walk because their payload is the same type and
// the same hazard: an ir.RawValue is copied into the output byte for byte, so a
// payload that is not a JSON value fails json.Marshal for the whole document
// (invariant #7) — one bad entry anywhere costs the entire artifact, and the
// error names json.RawMessage rather than the entry that carried it. RawConfig
// is the half nothing guarded at all: its five carriers are where an AsyncAPI
// compiler will write protocol bindings.
//
// Entries need no ordering first: each violation carries its key in Path, and
// Verify orders the whole result by (Code, Path) before returning it. The bool
// reports whether the bounded walk was cut short; Verify folds that into the
// document's one ir/walk-truncated violation.
func checkRawPayloads(doc *ir.Document) ([]Violation, bool) {
	var vs []Violation
	truncated := ir.WalkValues(doc, "doc", func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Map {
			return true
		}
		switch v.Type() {
		case unmodeledType:
			vs = appendEntries(vs, v, path, unmodeledEntry)
		case rawConfigType:
			vs = appendEntries(vs, v, path, rawConfigEntry)
		default:
			return true // any other map may still hold one of these below it
		}
		return false // entries hold no references and no nested payload map
	})
	return vs, truncated
}

// appendEntries appends check's verdict on every entry of the payload map m to
// vs. Both callers key on a plain string, so the key spells the entry's path.
func appendEntries(vs []Violation, m reflect.Value, path string,
	check func(key string, entry reflect.Value, path string) []Violation,
) []Violation {
	iter := m.MapRange()
	for iter.Next() {
		vs = append(vs, check(iter.Key().String(), iter.Value(), path)...)
	}
	return vs
}

// unmodeledEntry checks one entry of an Unmodeled map: a non-empty
// origin-namespaced key, a marshalable payload, and a declared UnmodeledReason.
// Consumers select entries by reason (see ir.UnmodeledEntry.Reason), so an entry
// left at the zero reason is bytes nothing can route, and one keyed "" cannot be
// looked up and is overwritten by the next empty-keyed entry. Both are compiler
// bugs of the same class checkRegistryKeys reports as ir/empty-*-id.
//
// The three are checked independently so a doubly-broken entry names each of its
// defects rather than stopping at the first.
func unmodeledEntry(key string, entry reflect.Value, path string) []Violation {
	at := path + "[" + key + "]"
	var vs []Violation
	if key == "" {
		at = path + `[""]`
		vs = append(vs, Violation{
			Code:    "ir/empty-unmodeled-key",
			Message: "unmodeled map has an empty key",
			Path:    at,
		})
	}
	vs = appendRawValue(vs, entry.FieldByName("Value"), "unmodeled entry", at)

	reason := ir.UnmodeledReason(entry.FieldByName("Reason").String())
	if reason == "" {
		return append(vs, Violation{
			Code:    "ir/empty-unmodeled-reason",
			Message: "unmodeled entry leaves its reason at the zero value",
			Path:    at,
		})
	}
	if !reason.Valid() {
		vs = append(vs, Violation{
			Code:    "ir/unknown-unmodeled-reason",
			Message: "unmodeled entry carries undeclared reason " + string(reason),
			Path:    at,
		})
	}
	return vs
}

// rawConfigEntry checks one entry of a RawConfig map. Unlike an Unmodeled entry
// it carries no reason to check — the source declared it where the IR expects it
// — so the payload is all there is to hold it to.
func rawConfigEntry(key string, entry reflect.Value, path string) []Violation {
	return appendRawValue(nil, entry, "protocol config", path+"["+key+"]")
}

// appendRawValue appends a violation to vs when the payload at value is not a
// JSON value, naming its carrier in what.
//
// Reading the bytes through reflect.Value.Bytes rather than Interface() keeps
// Verify a report-only oracle: the caller has already matched the map's exact
// type, so the payload is a byte slice by construction, and Bytes never panics
// on one however it was reached.
//
// Nil is reported alongside malformed and empty bytes. json.Marshal survives a
// nil — it encodes as null — but the value comes back as the four bytes "null",
// so a document carrying one stops round-tripping to an equal document, and an
// entry with no payload preserves no construct in the first place.
func appendRawValue(vs []Violation, value reflect.Value, what, path string) []Violation {
	if raw := value.Bytes(); json.Valid(raw) {
		return vs
	}
	return append(vs, Violation{
		Code:    "ir/invalid-raw-value",
		Message: what + " value is not a JSON value, so the document cannot be marshaled",
		Path:    path,
	})
}
