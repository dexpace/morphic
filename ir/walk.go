package ir

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// MaxWalkDepth bounds a [WalkValues] traversal (the bounded-recursion rule).
// Value trees — defaults, examples — nest deepest, and compilers cap their
// nesting far below this (the OpenAPI compiler at 128), so reaching the cap
// signals a pathological document rather than legitimate nesting. The walk
// reports truncation so a caller can say so instead of under-checking in
// silence.
const MaxWalkDepth = 4096

// MapKeySuffix ends the path of a value reached as a map key rather than as a
// field or an element, so a caller that treats the two differently can tell
// them apart.
const MapKeySuffix = ".key"

// WalkValues performs a bounded, cycle-guarded reflection traversal of root,
// calling visit on every value it reaches together with the path it was reached
// by; returning false from visit skips that value's children. It reports
// whether the depth cap cut the walk short.
//
// Deriving what a document holds from the value graph instead of naming fields
// is what makes a check built on it complete: a field added to the IR is covered
// the moment it exists, which a hand-written enumeration cannot promise. Map
// keys are reached as well as values, because some are references in their own
// right — Service.Renames is map[TypeID]Naming, where the key is the reference
// and the value is not.
//
// Paths spell fields joined by ".", slice indices and map keys in brackets, and
// an embedded field contributes no segment of its own: JSON inlines it and Go
// promotes its fields, so "….TypeCommon.Examples[0]" names a step neither
// encoding has — the example is reached as "….Examples[0]" in both. Both
// checkers report a dangling reference under one code, and a caller running both
// can only dedupe them if one defect reads as one location.
//
// A value reached through an unexported field is read-only, and Interface()
// panics on one where FieldByName does not, so a visitor reads the fields it
// needs rather than converting the value back to its Go type. That is what lets
// a caller be an oracle that never crashes on a malformed document.
//
// Map entries are visited in rendered-key order rather than Go's randomized map
// order. A pointer reachable from two entries is descended into at whichever the
// walk reaches first, so a random order yields a different path for it — and so
// a different result set, not merely a different order — on each run, which no
// later sort can repair (invariant 7).
//
// Byte sequences are skipped. Unmodeled and RawConfig payloads are
// json.RawMessage and are the largest values a document holds, while a uint8
// element is none of the things a visitor looks for — no typed ID, no Unmodeled
// map, no Provenance, no index carrier. Descending one costs a reflect.Value and
// a formatted path per byte for nothing: verifying a document holding one 256 KB
// payload measured 88ms without this skip against 22µs with it, for the same
// result. Since the result is the same either way, a test asserting the result
// cannot notice the skip going missing — one that counts what the walk reaches
// is what holds it.
func WalkValues(root any, path string, visit func(v reflect.Value, path string) bool) bool {
	w := valueWalk{seen: map[uintptr]bool{}, visit: visit}
	w.walk(reflect.ValueOf(root), path, 0)
	return w.truncated
}

// valueWalk is one walk's state: the pointers already followed, the visitor, and
// whether the depth cap cut the walk short.
//
// It is a value being walked rather than a walker being configured, so it is
// built at the entry point and discarded with it. Nothing here is reentrant and
// nothing outside this file holds one.
type valueWalk struct {
	seen      map[uintptr]bool
	visit     func(v reflect.Value, path string) bool
	truncated bool
}

// walk visits v and then descends into whatever it holds, stopping wherever the
// visitor says it has seen enough. The invalid zero Value — a nil root, a nil
// interface's dynamic value — is not visited, so no visitor has to guard against
// calling Type() on one.
func (w *valueWalk) walk(v reflect.Value, path string, depth int) {
	if !v.IsValid() || !w.visit(v, path) {
		return
	}
	w.children(v, path, depth)
}

// descend continues into a child unless that would pass the depth cap, which it
// records rather than reports: the caller decides whether a truncated walk is a
// finding, and it is the only one that knows what was being checked.
func (w *valueWalk) descend(child reflect.Value, path string, depth int) {
	if depth > MaxWalkDepth {
		w.truncated = true
		return
	}
	w.walk(child, path, depth)
}

// children descends into every child of v. It is where the walk's shape lives —
// what counts as a child of a pointer, an interface, a struct, a sequence and a
// map, and which path each child is addressed by. Numbers, bools, strings,
// funcs and channels have none.
func (w *valueWalk) children(v reflect.Value, path string, depth int) {
	switch v.Kind() {
	case reflect.Pointer:
		w.pointer(v, path, depth)
	case reflect.Interface:
		if !v.IsNil() {
			w.descend(v.Elem(), path, depth+1)
		}
	case reflect.Struct:
		t := v.Type()
		for i := range v.NumField() {
			w.descend(v.Field(i), fieldPath(path, t.Field(i)), depth+1)
		}
	case reflect.Slice, reflect.Array:
		w.sequence(v, path, depth)
	case reflect.Map:
		for _, e := range orderedEntries(v) {
			w.descend(e.key, fmt.Sprintf("%s[%s]%s", path, e.label, MapKeySuffix), depth+1)
			w.descend(e.value, fmt.Sprintf("%s[%s]", path, e.label), depth+1)
		}
	}
}

// pointer descends through a non-nil pointer once. The seen set terminates
// cyclic graphs — normal input for schema languages, not an edge case — and
// means a value reached through a shared pointer is visited once rather than
// once per reference to it.
//
// Which of an aliased pointer's referrers wins that single visit is decided by
// traversal order, so the walk's order has to be fixed rather than incidental;
// orderedEntries is where that is arranged.
func (w *valueWalk) pointer(v reflect.Value, path string, depth int) {
	if v.IsNil() {
		return
	}
	p := v.Pointer()
	if w.seen[p] {
		return
	}
	w.seen[p] = true
	w.descend(v.Elem(), path, depth+1)
}

// sequence descends into slice and array elements, skipping byte sequences (see
// [WalkValues] for why they are skipped and how the skip is held).
func (w *valueWalk) sequence(v reflect.Value, path string, depth int) {
	if v.Type().Elem().Kind() == reflect.Uint8 {
		return
	}
	for i := range v.Len() {
		w.descend(v.Index(i), fmt.Sprintf("%s[%d]", path, i), depth+1)
	}
}

// fieldPath extends path with f's name, except for an embedded field, which
// contributes no segment (see [WalkValues]).
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
// same rendering the path uses keeps the two in step, and it is a total order
// for every key type the IR declares: named string types, plain strings and ints
// all render distinct keys distinctly.
func orderedEntries(v reflect.Value) []mapEntry {
	entries := make([]mapEntry, 0, v.Len())
	for iter := v.MapRange(); iter.Next(); {
		k := iter.Key()
		entries = append(entries, mapEntry{label: fmt.Sprintf("%v", k), key: k, value: iter.Value()})
	}
	slices.SortFunc(entries, func(a, b mapEntry) int { return strings.Compare(a.label, b.label) })
	return entries
}
