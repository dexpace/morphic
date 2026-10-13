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

// DocumentPath is the path a walk rooted at a whole [Document] starts from, and
// so the first segment of every location such a walk reports. It is a constant
// rather than each caller's own string literal for the reason the rest of the
// path grammar is one walk: two checkers reporting one defect can only be
// deduped by a caller running both if the defect reads as one location.
const DocumentPath = "doc"

// WalkValues walks root by reflection, bounded and cycle-guarded, calling the
// non-nil visit on each value with its path; false skips that value's children.
// It reports whether the depth cap truncated the walk.
//
// Reflection covers a new IR field at once. Map keys are reached too, since
// some are references; entries come in rendered-key order, and byte elements
// are skipped.
//
// Paths start at path, join fields with "." and bracket indices and keys;
// embedded fields add no segment. A pointer reachable twice is entered once, at
// the first path reached. Interface() panics behind an unexported field, so a
// visitor reads fields instead.
func WalkValues(root any, path string, visit func(v reflect.Value, path string) bool) bool {
	w := valueWalk{seen: map[refKey]struct{}{}, visit: visit}
	w.walk(reflect.ValueOf(root), path, 0)
	return w.truncated
}

// ValueCycle is a place a walk met a pointer, slice or map it was already
// inside, so that the value contains itself and has no finite encoding.
type ValueCycle struct {
	// Path is where the walk met the reference the second time.
	Path string
	// Entered is where the walk first entered it: an ancestor of Path.
	Entered string
	// Type is the reference's Go type, such as *ir.CtorValue or []ir.Value.
	Type string
}

// ValueCycles walks root as [WalkValues] does and returns every [ValueCycle] it
// meets, in walk order, with whether the depth cap truncated the walk.
//
// Neither walk enters a pointer, slice or map from inside itself. A reference
// two places share without either holding the other is not a cycle: it encodes
// twice without trouble, and a slice or map so shared is walked from each place,
// so a cycle inside it is reported once per way in.
func ValueCycles(root any, path string) (cycles []ValueCycle, truncated bool) {
	w := valueWalk{
		seen:  map[refKey]struct{}{},
		visit: func(reflect.Value, string) bool { return true },
		cycle: func(c ValueCycle) { cycles = append(cycles, c) },
	}
	w.walk(reflect.ValueOf(root), path, 0)
	return cycles, w.truncated
}

// refKey identifies a reference the way the JSON encoder does, so that what a
// walk calls a cycle is what the encoder refuses. A pointer to a struct and a
// pointer to its first field share an address and differ in type; a prefix of a
// slice shares a backing array and differs in length.
type refKey struct {
	typ reflect.Type
	ptr uintptr
	n   int // a slice's length; zero for a pointer or a map
}

// insideRef is a reference the walk is inside, and the path it entered it at.
type insideRef struct {
	key     refKey
	entered string
}

// valueWalk is one walk's state: the pointers already entered, the references
// the walk is inside right now, the visitor, the cycle callback if the caller
// wants cycles, and whether the depth cap cut the walk short.
//
// It is a value being walked rather than a walker being configured, so it is
// built at the entry point and discarded with it. Nothing here is reentrant and
// nothing outside this file holds one.
type valueWalk struct {
	seen      map[refKey]struct{}
	inside    []insideRef
	visit     func(v reflect.Value, path string) bool
	cycle     func(ValueCycle)
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
		// Indexed: ranging over v.Fields() puts the loop body on the heap for
		// every struct the walk enters.
		t := v.Type()
		for i := range v.NumField() {
			w.descend(v.Field(i), fieldPath(path, t.Field(i)), depth+1)
		}
	case reflect.Slice, reflect.Array:
		w.sequence(v, path, depth)
	case reflect.Map:
		w.mapping(v, path, depth)
	}
}

// pointer descends through a non-nil pointer once, so a value reached through
// a shared pointer is visited once rather than once per reference to it. One
// met again while the walk is still inside it closes a cycle, which
// closesCycle reports.
//
// Which of an aliased pointer's referrers wins that single visit is decided by
// traversal order, so the walk's order has to be fixed rather than incidental;
// orderedEntries is where that is arranged.
func (w *valueWalk) pointer(v reflect.Value, path string, depth int) {
	if v.IsNil() {
		return
	}
	key := refKey{typ: v.Type(), ptr: v.Pointer()}
	if _, met := w.seen[key]; met {
		w.closesCycle(key, path)
		return
	}
	w.seen[key] = struct{}{}
	w.enter(key, path)
	w.descend(v.Elem(), path, depth+1)
	w.leave()
}

// enter records that the walk is inside key, entered at path.
func (w *valueWalk) enter(key refKey, path string) {
	w.inside = append(w.inside, insideRef{key: key, entered: path})
}

// leave records that the walk has left the reference it entered last.
func (w *valueWalk) leave() {
	w.inside = w.inside[:len(w.inside)-1]
}

// closesCycle reports whether the walk is already inside key, which makes
// meeting it at path a cycle, and reports the cycle to the caller if it asked for
// them. The scan is as long as the references enclosing path — a handful in a
// compiled document and at most [MaxWalkDepth] in any.
func (w *valueWalk) closesCycle(key refKey, path string) bool {
	for _, ref := range w.inside {
		if ref.key != key {
			continue
		}
		if w.cycle != nil {
			w.cycle(ValueCycle{Path: path, Entered: ref.entered, Type: key.typ.String()})
		}
		return true
	}
	return false
}

// sequence descends into slice and array elements, skipping byte sequences.
// Unmodeled and RawConfig payloads are jsontext.Value, the largest values a
// document holds, and a uint8 element is nothing a visitor looks for, so
// descending costs a reflect.Value and a formatted path per byte: 88ms against
// 22µs for one 256 KB payload. The result is the same either way, so only a
// test counting what the walk reaches can notice the skip going missing.
//
// A slice is a reference; an array is a value, with no address of its own.
func (w *valueWalk) sequence(v reflect.Value, path string, depth int) {
	if v.Type().Elem().Kind() == reflect.Uint8 || v.Len() == 0 {
		return
	}
	if v.Kind() != reflect.Slice {
		w.elements(v, path, depth)
		return
	}
	key := refKey{typ: v.Type(), ptr: v.Pointer(), n: v.Len()}
	if w.closesCycle(key, path) {
		return
	}
	w.enter(key, path)
	w.elements(v, path, depth)
	w.leave()
}

// elements descends into each element of a slice or array.
func (w *valueWalk) elements(v reflect.Value, path string, depth int) {
	for i := range v.Len() {
		w.descend(v.Index(i), fmt.Sprintf("%s[%d]", path, i), depth+1)
	}
}

// mapping descends into a map's keys and values. A map is a reference.
func (w *valueWalk) mapping(v reflect.Value, path string, depth int) {
	if v.Len() == 0 {
		return
	}
	key := refKey{typ: v.Type(), ptr: v.Pointer()}
	if w.closesCycle(key, path) {
		return
	}
	w.enter(key, path)
	w.entries(v, path, depth)
	w.leave()
}

// entries descends into each key and value of a map, in rendered-key order.
func (w *valueWalk) entries(v reflect.Value, path string, depth int) {
	for _, e := range orderedEntries(v) {
		w.descend(e.key, fmt.Sprintf("%s[%s]%s", path, e.label, MapKeySuffix), depth+1)
		w.descend(e.value, fmt.Sprintf("%s[%s]", path, e.label), depth+1)
	}
}

// fieldPath extends path with f's name, except for an embedded field, which
// contributes no segment: JSON inlines it and Go promotes its fields, so
// "….TypeCommon.Examples[0]" would name a step neither encoding has.
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

// orderedEntries returns v's entries ordered by rendered key rather than Go's
// randomized map order. A pointer reachable from two entries is descended into
// at whichever the walk reaches first, so a random order would change the
// result set, not just its order, and no later sort repairs that (invariant 7).
// The rendering is the one the path uses, which keeps the two in step, and it
// is a total order for every key type the IR declares: named string types,
// plain strings and ints all render distinct keys distinctly.
func orderedEntries(v reflect.Value) []mapEntry {
	entries := make([]mapEntry, 0, v.Len())
	// MapRange rather than v.Seq2(), which puts the loop body on the heap.
	for iter := v.MapRange(); iter.Next(); {
		k := iter.Key()
		entries = append(entries, mapEntry{label: fmt.Sprintf("%v", k), key: k, value: iter.Value()})
	}
	slices.SortFunc(entries, func(a, b mapEntry) int { return strings.Compare(a.label, b.label) })
	return entries
}
