package irverify

import (
	"encoding/json/jsontext"
	"reflect"
	"strconv"

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
// Both are checked in one walk: their payload is the same type and the same
// hazard. An ir.RawValue is written into the output as the JSON it holds, so a
// payload that is not a JSON value fails the whole document's encoding
// (invariant #7).
//
// Verify sorts the result by (Code, Path), so entries need no ordering.
func checkRawPayloads(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Kind() != reflect.Map {
				return true
			}
			switch v.Type() {
			case unmodeledType:
				vs = appendEntries(vs, v, at, unmodeledEntry)
			case rawConfigType:
				vs = appendEntries(vs, v, at, rawConfigEntry)
			default:
				return true // any other map may still hold one of these below it
			}
			return false // entries hold no nested payload map
		},
		report: reporting(&vs),
	}
}

// appendEntries appends check's verdict on every entry of the payload map m, at
// at, to vs. Both callers key on a plain string, so the key spells the entry's
// path.
func appendEntries(vs []Violation, m reflect.Value, at ir.WalkPath,
	check func(key string, entry reflect.Value, at ir.WalkPath) []Violation,
) []Violation {
	iter := m.MapRange()
	for iter.Next() {
		vs = append(vs, check(iter.Key().String(), iter.Value(), at)...)
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
func unmodeledEntry(key string, entry reflect.Value, at ir.WalkPath) []Violation {
	var vs []Violation
	if key == "" {
		vs = append(vs, Violation{
			Code:    "ir/empty-unmodeled-key",
			Message: "unmodeled map has an empty key",
		})
	}
	vs = appendRawValue(vs, entry.FieldByName("Value"), "unmodeled entry")

	switch reason := ir.UnmodeledReason(entry.FieldByName("Reason").String()); {
	case reason == "":
		vs = append(vs, Violation{
			Code:    "ir/empty-unmodeled-reason",
			Message: "unmodeled entry leaves its reason at the zero value",
		})
	case !reason.Valid():
		vs = append(vs, Violation{
			Code:    "ir/unknown-unmodeled-reason",
			Message: "unmodeled entry carries undeclared reason " + strconv.Quote(string(reason)),
		})
	}
	return placeEntry(vs, at, key, `[""]`)
}

// rawConfigEntry checks one entry of a RawConfig map. Unlike an Unmodeled entry
// it carries no reason to check — the source declared it where the IR expects it
// — so the payload is all there is to hold it to.
func rawConfigEntry(key string, entry reflect.Value, at ir.WalkPath) []Violation {
	return placeEntry(appendRawValue(nil, entry, "protocol config"), at, key, "[]")
}

// placeEntry gives each of one entry's violations vs the entry's path: the map
// at at, then the key in brackets, or empty when the key is. The path is spelled
// only when there is something to place.
func placeEntry(vs []Violation, at ir.WalkPath, key, empty string) []Violation {
	if len(vs) == 0 {
		return nil
	}
	path := at.String() + "[" + key + "]"
	if key == "" {
		path = at.String() + empty
	}
	for i := range vs {
		vs[i].Path = path
	}
	return vs
}

// appendRawValue appends a violation to vs when the payload at value is not a
// JSON value, naming its carrier in what.
//
// Reading through reflect.Value.Bytes rather than Interface() keeps Verify a
// report-only oracle: the caller has matched the map's exact type, so Bytes
// never panics however the payload was reached.
//
// Nil is reported with malformed and empty bytes: a nil jsontext.Value encodes
// as null but decodes back as the four bytes "null", so the document stops
// round-tripping to an equal one.
func appendRawValue(vs []Violation, value reflect.Value, what string) []Violation {
	raw := value.Bytes()
	if jsontext.Value(raw).IsValid() {
		return vs
	}
	msg := what + " value is not a JSON value, so the document cannot be marshaled"
	if raw == nil {
		msg = what + " value is nil, which encodes as null and decodes back as the bytes null rather than nil"
	}
	return append(vs, Violation{Code: "ir/invalid-raw-value", Message: msg})
}
