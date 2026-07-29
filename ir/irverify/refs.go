package irverify

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// maxWalkDepth bounds the reflection traversal (bounded-recursion rule). Value
// trees (defaults, examples) are the deepest structures reached, and compilers
// cap their nesting (the OpenAPI compiler caps it at 128), so this limit sits
// well above what a validly-bounded document can produce — hitting it signals
// a pathological document, not legitimate nesting. walkValues reports
// truncation and checkReferentialIntegrity surfaces it as ir/walk-truncated.
const maxWalkDepth = 4096

// refSite is one discovered ID reference and the reference class (refKind) it
// must resolve in.
type refSite struct {
	id   string
	kind refKind
	path string
}

// resolves reports whether s.id exists in the registry s.kind names. The
// has == nil guard — rather than calling s.kind.has directly — keeps Verify a
// report-only oracle that never crashes on a malformed document, even if a
// refSite is ever built with a kind this package does not recognize.
func (s refSite) resolves(doc *ir.Document) bool {
	if s.kind.has == nil {
		return false
	}
	return s.kind.has(doc, s.id)
}

// refKind is one class of typed-ID reference this checker resolves: a
// registry label (for violation paths and messages), a diagnostic-code
// singular, and a resolves-in-registry test. refKindByType is the single
// source of truth for what counts as a "reference" here, so a class can't be
// silently dropped by missing it from one of several parallel lists.
//
// PropID and ServiceID are intentionally absent: neither lives in a
// document-level flat registry, so they are out of scope for Phase 1.
type refKind struct {
	registry string
	singular string
	has      func(doc *ir.Document, id string) bool
}

// refKindByType maps a typed-ID reflect.Type to its refKind. Literals below
// are keyed, not positional: registry and singular are both plain strings,
// and a positional row could silently swap them, emitting
// "ir/dangling-types-ref" instead of "ir/dangling-type-ref".
var refKindByType = map[reflect.Type]refKind{
	reflect.TypeOf(ir.TypeID("")): {registry: "types", singular: "type", has: func(doc *ir.Document, id string) bool {
		_, ok := doc.Types[ir.TypeID(id)]
		return ok
	}},
	reflect.TypeOf(ir.AuthID("")): {registry: "auth", singular: "auth", has: func(doc *ir.Document, id string) bool {
		_, ok := doc.Auth[ir.AuthID(id)]
		return ok
	}},
	reflect.TypeOf(ir.ChannelID("")): {registry: "channels", singular: "channel", has: func(doc *ir.Document, id string) bool {
		_, ok := doc.Channels[ir.ChannelID(id)]
		return ok
	}},
	reflect.TypeOf(ir.MessageID("")): {registry: "messages", singular: "message", has: func(doc *ir.Document, id string) bool {
		_, ok := doc.Messages[ir.MessageID(id)]
		return ok
	}},
}

// collectRefs walks doc and returns every non-empty typed-ID reference plus
// whether the bounded walk was truncated. It inspects struct fields, slice/array
// elements, and both map keys and values: most map keys are an entry's own ID
// and resolve trivially, but some — Service.Renames's map[TypeID]Naming keys —
// are genuine references into a registry that must resolve, so keys are
// collected too.
func collectRefs(doc *ir.Document) ([]refSite, bool) {
	var sites []refSite
	truncated := walkValues(doc, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.String {
			return true
		}
		if k, ok := refKindByType[v.Type()]; ok && v.String() != "" {
			sites = append(sites, refSite{id: v.String(), kind: k, path: path})
		}
		return true
	})
	return sites, truncated
}

// checkReferentialIntegrity asserts every discovered reference resolves in its
// registry, emitting one dangling-*-ref Violation per unresolved reference.
func checkReferentialIntegrity(doc *ir.Document) []Violation {
	var vs []Violation
	sites, truncated := collectRefs(doc)
	if truncated {
		vs = append(vs, Violation{
			Code:    "ir/walk-truncated",
			Message: "document nests deeper than the bounded verifier walk; some references and names went unchecked",
			Path:    "doc",
		})
	}
	for _, s := range sites {
		if s.resolves(doc) {
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/dangling-" + s.kind.singular + "-ref",
			Message: "reference " + s.id + " does not resolve in " + s.kind.registry,
			Path:    s.path,
		})
	}
	return vs
}

// walkValues performs a bounded, cycle-guarded reflection traversal of root,
// calling visit on every value it reaches; returning false from visit skips
// that value's children. Map keys are walked too, not just values — see
// collectRefs for why that matters.
//
// A value reached through an unexported field is read-only, and Interface()
// panics on one where FieldByName does not, so a visitor reads the fields it
// needs rather than converting the value back to its Go type. That is what keeps
// Verify an oracle that never crashes on a malformed document.
//
// Map entries are visited in rendered-key order rather than Go's randomized map
// order. A pointer reachable from two entries is descended into at whichever the
// walk reaches first, so a random order yields a different path for it — and so a
// different violation set, not merely a different order — on each run, which
// Verify's final sort cannot repair (invariant 7).
//
// Byte sequences are skipped, matching pass.refWalk.walkSequence. Preserved and
// RawConfig payloads are json.RawMessage and are the largest values a document
// holds, while a uint8 element is none of the things a visitor here looks for —
// no typed ID, no Preserved map, no Provenance, no index carrier. Descending one
// costs a reflect.Value and a formatted path per byte for nothing: Verify over a
// document holding one 256 KB payload measured 88ms without this skip against
// 22µs with it, for the same result. Both walks skip it; keeping the note in
// both is what stops one of them quietly growing the cost back.
//
// The bool return is true only when the depth cap truncated the walk, so
// callers can surface a too-deep document instead of silently
// under-checking it.
func walkValues(root any, visit func(v reflect.Value, path string) bool) bool {
	seen := map[uintptr]bool{}
	truncated := false
	var walk func(v reflect.Value, path string, depth int)
	descend := func(child reflect.Value, path string, depth int) {
		if depth > maxWalkDepth {
			truncated = true
			return
		}
		walk(child, path, depth)
	}
	walk = func(v reflect.Value, path string, depth int) {
		if !v.IsValid() || !visit(v, path) {
			return
		}
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			p := v.Pointer()
			if seen[p] {
				return
			}
			seen[p] = true
			descend(v.Elem(), path, depth+1)
		case reflect.Interface:
			if !v.IsNil() {
				descend(v.Elem(), path, depth+1)
			}
		case reflect.Struct:
			for i := range v.NumField() {
				descend(v.Field(i), fieldPath(path, v.Type().Field(i)), depth+1)
			}
		case reflect.Slice, reflect.Array:
			if v.Type().Elem().Kind() == reflect.Uint8 {
				return // byte sequences hold nothing any visitor looks for
			}
			for i := range v.Len() {
				descend(v.Index(i), fmt.Sprintf("%s[%d]", path, i), depth+1)
			}
		case reflect.Map:
			for _, e := range orderedEntries(v) {
				descend(e.key, fmt.Sprintf("%s[%s].key", path, e.label), depth+1)
				descend(e.value, fmt.Sprintf("%s[%s]", path, e.label), depth+1)
			}
		}
	}
	walk(reflect.ValueOf(root), "doc", 0)
	return truncated
}

// fieldPath extends path with f's name, except for an embedded field, which
// contributes no segment: JSON inlines it and Go promotes its fields, so
// "….TypeCommon.Examples[0]" names a step neither encoding has — the example is
// reached as "….Examples[0]" in both.
func fieldPath(path string, f reflect.StructField) string {
	if f.Anonymous {
		return path
	}
	return path + "." + f.Name
}

// mapEntry is one map entry paired with its rendered key, which both spells the
// entry's path and orders the walk.
type mapEntry struct {
	label string
	key   reflect.Value
	value reflect.Value
}

// orderedEntries returns v's entries ordered by rendered key. Ordering by the
// same rendering the path uses keeps the two in step, and it is a total order for
// every key type the IR declares: named string types, plain strings and ints all
// render distinct keys distinctly.
func orderedEntries(v reflect.Value) []mapEntry {
	entries := make([]mapEntry, 0, v.Len())
	for iter := v.MapRange(); iter.Next(); {
		k := iter.Key()
		entries = append(entries, mapEntry{label: fmt.Sprintf("%v", k), key: k, value: iter.Value()})
	}
	slices.SortFunc(entries, func(a, b mapEntry) int { return strings.Compare(a.label, b.label) })
	return entries
}
