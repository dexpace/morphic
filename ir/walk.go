// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import (
	"context"
	"math/bits"
	"reflect"
	"slices"
	"strconv"
)

var _ Visitor = VisitFunc(nil)

// MaxWalkDepth bounds a [WalkValues] traversal (the bounded-recursion rule).
// Value trees — defaults, examples — nest deepest, and compilers cap their
// nesting far below this (the OpenAPI compiler at 128), so reaching the cap
// signals a pathological document rather than legitimate nesting. The walk
// reports truncation so a caller can say so instead of under-checking in
// silence.
const MaxWalkDepth = 4096

// MaxWalkValues bounds what a [WalkValues] traversal shows each visitor, so a
// document whose references unfold far past its own size, a slice or pointer
// held twice a level, costs at most this much to walk. It is many times what
// the largest compiled document shows a visitor. A visitor reaching it is shown
// nothing more, and the walk reports truncation, as at the depth cap.
const MaxWalkValues = 1 << 27

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

// Visitor is shown each value a [WalkValues] traversal reaches.
type Visitor interface {
	// Visit is called on v, reached by the path at, and reports whether this
	// visitor is to be shown what v holds; false prunes v's children for it
	// alone.
	//
	// A map's key or value, with all it holds by value, is the walk's own copy,
	// as Go gives a map entry no address: setting it changes nothing in the
	// map, and the walk reuses it once it leaves the map, so a visitor keeps
	// what it reads, not v, as it keeps a path's String. What a pointer, slice,
	// map or interface in it leads to is the document's own.
	Visit(v reflect.Value, at WalkPath) bool
}

// VisitFunc adapts a function to a [Visitor]. A nil one prunes at the root.
type VisitFunc func(v reflect.Value, at WalkPath) bool

// Visit calls f.
func (f VisitFunc) Visit(v reflect.Value, at WalkPath) bool {
	return f != nil && f(v, at)
}

// CycleVisitor is a [Visitor] that is also told where its walk meets a pointer,
// slice or map it is already inside: a value that contains itself, which has no
// finite encoding. The walk asks each visitor whether it is one, so a type
// meant to be one carries var _ CycleVisitor, which fails the build when a
// method goes, and internal/archtest requires it.
type CycleVisitor interface {
	Visitor
	// Cycle is called at at, where the walk meets again a reference of type ref
	// it entered at entered, an ancestor of at. The walk does not enter it again.
	Cycle(at, entered WalkPath, ref reflect.Type)
}

// WalkValues walks root by reflection, showing every value to each visitor. It
// reads what root's encoding writes: a pointer, slice or map from every holder,
// cut where it closes a cycle, at most MaxWalkDepth deep.
//
// Visitors share one traversal, or one per 64, called in order, a nil one
// skipped; each sees the walk it would see alone, its own pruning and its own
// limit of MaxWalkValues included. The result is whether any was shown less
// than the whole.
//
// Map keys are reached, some being references, in rendered-key order; bytes
// are skipped. Interface() panics behind an unexported field: read fields.
func WalkValues(root any, path string, visitors ...Visitor) bool {
	return walkValues(nil, MaxWalkValues, root, path, visitors).truncated
}

// WalkValuesContext is [WalkValues] stopped once ctx is done. It asks ctx before
// the first value and every askEvery values after, and once refused shows no
// visitor another value, in any traversal. A walk it stopped has answered in
// part, so ctx's error comes back beside whether a bound truncated it.
func WalkValuesContext(ctx context.Context, root any, path string, visitors ...Visitor) (bool, error) {
	out := walkValues(ctx.Err, MaxWalkValues, root, path, visitors)
	return out.truncated, out.err
}

// askEvery is how many values a walk shows between two asks of its context: a
// value costs a visit or two, so asking at each would cost more than the
// visits, and a stretch this long still ends within microseconds.
const askEvery = 256

// walkEnd is how a walk ended: whether a bound truncated a traversal, and the
// error of the ask that stopped one, which ends the walk.
type walkEnd struct {
	truncated bool
	err       error
}

// walkValues walks root for visitors, one traversal per maxSharedVisitors of
// them, showing each at most limit values, and asking ask whether to stop as
// WalkValuesContext says, or never for a nil ask.
func walkValues(ask func() error, limit int, root any, path string, visitors []Visitor) walkEnd {
	v := reflect.ValueOf(root)
	var out walkEnd
	for group := range slices.Chunk(visitors, maxSharedVisitors) {
		w := newValueWalk(path, group)
		w.ask, w.limit = ask, limit
		w.walk(v, 0, w.all)
		out.truncated = out.truncated || w.truncated
		if w.refusal != nil {
			out.err = w.refusal
			return out
		}
	}
	return out
}

// maxSharedVisitors is how many visitors one traversal carries: the width of
// [visitorSet]. [WalkValues] carries any more in further traversals, as many to
// each, and each visitor sees the same walk either way.
const maxSharedVisitors = 64

// visitorSet holds which of a traversal's visitors a value is shown to, one
// bit each by position.
type visitorSet uint64

// WalkPath is the path by which a walk reached the value it is visiting. Paths
// start at the walk's root path, join fields with "." and bracket indices and
// keys; embedded fields add no segment.
//
// A WalkPath reads the walk's own state, and nothing renders it until String or
// AppendTo is called, so a visit that never reads its path pays nothing for it.
// It is valid until the call it was passed to returns, or, replayed by a
// [PathLog], until the next is yielded; a visitor keeps its String, or records
// it in a PathLog. The zero WalkPath is empty.
type WalkPath struct {
	walk *valueWalk
	// depth is how many of the walk's steps lead to the value.
	depth int
}

// String renders the path.
func (p WalkPath) String() string {
	// Rendered on the stack, so the string is the one allocation. Paths through
	// the largest documents compiled so far stay under 400 bytes, and a longer
	// one costs only a second allocation.
	var buf [512]byte
	return string(p.AppendTo(buf[:0]))
}

// AppendTo appends the rendered path to b and returns the extended buffer, so a
// caller can look a path up without allocating one.
func (p WalkPath) AppendTo(b []byte) []byte {
	if p.walk == nil {
		return b
	}
	b = append(b, p.walk.root...)
	for _, s := range p.walk.steps[:p.depth] {
		b = s.appendTo(b)
	}
	return b
}

// Field reports the struct field the path ends in, as String spells it: the
// struct type the field is spelled on and the field's name, or false when the
// path ends in an element, a map entry or the root. A field reached through an
// embedded struct, promoted or shadowed, is spelled on the outermost struct
// embedding it, so a Primitive's Name is reported as Primitive's, and an
// embedded struct, which the path does not spell, reports what reached the
// struct embedding it.
func (p WalkPath) Field() (owner reflect.Type, name string, ok bool) {
	if p.walk == nil {
		return nil, "", false
	}
	steps := p.walk.steps[:p.depth]
	last := len(steps) - 1
	for last >= 0 && steps[last].embedded() {
		last--
	}
	if last < 0 || steps[last].kind != stepField {
		return nil, "", false
	}
	owner = steps[last].owner
	for i := last - 1; i >= 0 && steps[i].embedded(); i-- {
		owner = steps[i].owner
	}
	return owner, steps[last].owner.Field(int(steps[last].n)).Name, true
}

// IsMapKey reports whether the path ends in a map entry's key, which String
// spells with [MapKeySuffix], so a caller can tell a key from the entry's value
// without spelling the path.
func (p WalkPath) IsMapKey() bool {
	return p.walk != nil && p.depth > 0 && p.walk.steps[p.depth-1].kind == stepMapKey
}

// stepKind is how a value is reached from its parent.
type stepKind uint8

const (
	stepField    stepKind = iota // a struct field
	stepElement                  // a slice or array element
	stepMapKey                   // a map entry's key
	stepMapValue                 // a map entry's value
)

// pathStep is one step of a walk's path, held as what it was taken through
// rather than as text, so taking it costs no string: a struct field by its
// owner and index, an element by its index, an entry by its key as the map's
// entry holds it, a label or an integer.
type pathStep struct {
	owner reflect.Type
	// n is the field's or the element's index, or an integer key as form holds
	// it, whichever kind of step this is.
	n     uint64
	label string
	kind  stepKind
	form  keyForm
}

// appendTo renders the step onto b. A field read through an embedded struct is
// promoted by Go and inlined by JSON, so the embedded field itself renders
// nothing: "….TypeCommon.Examples[0]" would name a step neither encoding has.
func (s pathStep) appendTo(b []byte) []byte {
	switch s.kind {
	case stepField:
		if f := s.owner.Field(int(s.n)); !f.Anonymous {
			b = append(append(b, '.'), f.Name...)
		}
	case stepElement:
		b = append(strconv.AppendUint(append(b, '['), s.n, 10), ']')
	case stepMapKey:
		b = append(append(appendKey(append(b, '['), s.form, s.n, s.label), ']'), MapKeySuffix...)
	case stepMapValue:
		b = append(appendKey(append(b, '['), s.form, s.n, s.label), ']')
	}
	return b
}

// embedded reports whether s is a step into an embedded struct, which a path
// does not spell.
func (s pathStep) embedded() bool {
	return s.kind == stepField && s.owner.Field(int(s.n)).Anonymous
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

// insideRef is a reference the walk is inside, and how many steps led to it.
type insideRef struct {
	key   refKey
	depth int
}

// valueWalk is one traversal's state: its visitors, how many values each has
// been shown, the references and steps the walk is inside right now, the map
// entries it is ranging over, and whether a bound or its ask cut it short.
//
// It is a value being walked rather than a walker being configured, so it is
// built at the entry point and discarded with it. Nothing here is reentrant,
// and outside this file only a WalkPath holds one, for the call it is handed to.
type valueWalk struct {
	root     string
	visitors []Visitor
	cycles   []CycleVisitor
	all      visitorSet
	// limit is how many values a visitor may be shown, and cut the visitors that
	// reached it. The traversal counts the values it shows any visitor, and each
	// visitor's pruned the ones it showed inside a subtree that visitor pruned,
	// so a visitor has been shown count less its pruned (counted).
	limit     int
	count     int
	pruned    [maxSharedVisitors]int
	cut       visitorSet
	inside    []insideRef
	steps     []pathStep
	ranging   entryStack
	truncated bool
	// ask is what the walk asks whether to stop (WalkValuesContext), or nil for
	// a walk to the end. shown counts the values shown, which spaces the asks,
	// and refusal is the answer that stopped it, after which nothing is shown.
	ask     func() error
	shown   int
	refusal error
}

// newValueWalk builds the state of one traversal from root showing visitors,
// of which there are at most maxSharedVisitors. The step stack is sized up front
// so that a walk deeper than a struct or two costs no allocation per level.
func newValueWalk(root string, visitors []Visitor) *valueWalk {
	w := &valueWalk{
		root:     root,
		visitors: visitors,
		cycles:   make([]CycleVisitor, len(visitors)),
		steps:    make([]pathStep, 0, 32),
	}
	for i, v := range visitors {
		if v == nil {
			continue
		}
		w.all |= 1 << i
		w.cycles[i], _ = v.(CycleVisitor)
	}
	return w
}

// here is the path to the value the walk is at.
func (w *valueWalk) here() WalkPath {
	return WalkPath{walk: w, depth: len(w.steps)}
}

// walk shows v to the visitors in active and descends into what it holds for
// those that ask. A visitor shown its limit is shown nothing more, which cuts
// the walk short. The invalid zero Value — a nil root, a nil interface's
// dynamic value — is not shown, so no visitor has to guard against calling
// Type() on one.
func (w *valueWalk) walk(v reflect.Value, depth int, active visitorSet) {
	if !v.IsValid() || w.refused() {
		return
	}
	if active &^= w.cut; active != 0 && w.count >= w.limit {
		active = w.cutAt(active)
	}
	if active == 0 {
		return
	}
	w.count++
	at := w.here()
	var asking visitorSet
	for set := active; set != 0; set &= set - 1 {
		i := bits.TrailingZeros64(uint64(set))
		if w.visitors[i].Visit(v, at) {
			asking |= 1 << i
		}
	}
	if asking == 0 {
		return
	}
	before := w.count
	w.children(v, depth, asking)
	if pruning := active &^ asking; pruning != 0 && w.count > before {
		w.prune(pruning, w.count-before)
	}
}

// cutAt returns active less each visitor in it that has been shown its limit,
// which it records as cut. A visitor in active is inside no subtree it pruned,
// whose counts are all added to its pruned, so count less pruned is exact.
func (w *valueWalk) cutAt(active visitorSet) visitorSet {
	for set := active; set != 0; set &= set - 1 {
		i := bits.TrailingZeros64(uint64(set))
		if w.count-w.pruned[i] >= w.limit {
			w.cut |= 1 << i
			w.truncated = true
		}
	}
	return active &^ w.cut
}

// prune adds n, the values shown beneath a value the visitors in pruning were
// shown and pruned, to what each of them was not shown.
func (w *valueWalk) prune(pruning visitorSet, n int) {
	for set := pruning; set != 0; set &= set - 1 {
		w.pruned[bits.TrailingZeros64(uint64(set))] += n
	}
}

// refused reports whether the walk has stopped at its ask, asking before the
// first value it shows and every askEvery values after.
func (w *valueWalk) refused() bool {
	if w.ask == nil || w.refusal != nil {
		return w.refusal != nil
	}
	if w.shown%askEvery == 0 {
		if w.refusal = w.ask(); w.refusal != nil {
			return true
		}
	}
	w.shown++
	return false
}

// step descends into child, reached from its parent by s.
func (w *valueWalk) step(s pathStep, child reflect.Value, depth int, active visitorSet) {
	w.steps = append(w.steps, s)
	w.descend(child, depth, active)
	w.steps = w.steps[:len(w.steps)-1]
}

// descend continues into a child unless that would pass the depth cap, which it
// records rather than reports, as walk records a visitor's limit: the caller
// decides whether a truncated walk is a finding, and it is the only one that
// knows what was being checked.
func (w *valueWalk) descend(child reflect.Value, depth int, active visitorSet) {
	if depth > MaxWalkDepth {
		w.truncated = true
		return
	}
	w.walk(child, depth, active)
}

// children descends into every child of v. It is where the walk's shape lives —
// what counts as a child of a pointer, an interface, a struct, a sequence and a
// map, and which step reaches each child. Numbers, bools, strings, funcs and
// channels have none.
func (w *valueWalk) children(v reflect.Value, depth int, active visitorSet) {
	switch v.Kind() {
	case reflect.Pointer:
		w.pointer(v, depth, active)
	case reflect.Interface:
		if !v.IsNil() {
			w.descend(v.Elem(), depth+1, active)
		}
	case reflect.Struct:
		// Indexed: ranging over v.Fields() puts the loop body on the heap for
		// every struct the walk enters.
		t := v.Type()
		for i := range v.NumField() {
			w.step(pathStep{kind: stepField, owner: t, n: uint64(i)}, v.Field(i), depth+1, active)
		}
	case reflect.Slice, reflect.Array:
		w.sequence(v, depth, active)
	case reflect.Map:
		w.mapping(v, depth, active)
	}
}

// pointer descends through a non-nil pointer, as sequence and mapping descend
// into a slice and a map: a value two holders share is read from each, as
// root's encoding writes it at each. One met again while the walk is still
// inside it closes a cycle, which closesCycle reports.
func (w *valueWalk) pointer(v reflect.Value, depth int, active visitorSet) {
	if v.IsNil() {
		return
	}
	key := refKey{typ: v.Type(), ptr: v.Pointer()}
	if w.closesCycle(key, active) {
		return
	}
	w.enter(key)
	w.descend(v.Elem(), depth+1, active)
	w.leave()
}

// enter records that the walk is inside key, entered at the path it is at.
func (w *valueWalk) enter(key refKey) {
	w.inside = append(w.inside, insideRef{key: key, depth: len(w.steps)})
}

// leave records that the walk has left the reference it entered last.
func (w *valueWalk) leave() {
	w.inside = w.inside[:len(w.inside)-1]
}

// closesCycle reports whether the walk is already inside key, which makes
// meeting it here a cycle, and tells the visitors in active that ask for cycles.
// The scan is as long as the references enclosing the walk — a handful in a
// compiled document and at most [MaxWalkDepth] in any.
//
// Every visitor in active entered key where the walk did, since a visitor
// leaves the walk's active set below a value only by pruning it. None is cut:
// walk drops a cut visitor from the set it hands the value's children.
func (w *valueWalk) closesCycle(key refKey, active visitorSet) bool {
	for _, ref := range w.inside {
		if ref.key != key {
			continue
		}
		at, entered := w.here(), WalkPath{walk: w, depth: ref.depth}
		for set := active; set != 0; set &= set - 1 {
			if c := w.cycles[bits.TrailingZeros64(uint64(set))]; c != nil {
				c.Cycle(at, entered, key.typ)
			}
		}
		return true
	}
	return false
}

// sequence descends into slice and array elements, skipping byte sequences.
// Unmodeled and RawConfig payloads are jsontext.Value, the largest values a
// document holds, and a uint8 element is nothing a visitor looks for, so
// descending costs a reflect.Value and a visit per byte: 88ms against 22µs for
// one 256 KB payload. The result is the same either way, so only a test
// counting what the walk reaches can notice the skip going missing.
//
// A slice is a reference; an array is a value, with no address of its own.
func (w *valueWalk) sequence(v reflect.Value, depth int, active visitorSet) {
	if v.Type().Elem().Kind() == reflect.Uint8 || v.Len() == 0 {
		return
	}
	if v.Kind() != reflect.Slice {
		w.elements(v, depth, active)
		return
	}
	key := refKey{typ: v.Type(), ptr: v.Pointer(), n: v.Len()}
	if w.closesCycle(key, active) {
		return
	}
	w.enter(key)
	w.elements(v, depth, active)
	w.leave()
}

// elements descends into each element of a slice or array.
func (w *valueWalk) elements(v reflect.Value, depth int, active visitorSet) {
	for i := range v.Len() {
		w.step(pathStep{kind: stepElement, n: uint64(i)}, v.Index(i), depth+1, active)
	}
}

// mapping descends into a map's keys and values. A map is a reference.
func (w *valueWalk) mapping(v reflect.Value, depth int, active visitorSet) {
	if v.Len() == 0 {
		return
	}
	key := refKey{typ: v.Type(), ptr: v.Pointer()}
	if w.closesCycle(key, active) {
		return
	}
	w.enter(key)
	w.entries(v, depth, active)
	w.leave()
}

// entries descends into each key and value of a map, in rendered-key order.
// The entries sit on one stack the walk reuses: a map nested inside an entry
// stacks its own above them and takes them off again.
func (w *valueWalk) entries(v reflect.Value, depth int, active visitorSet) {
	f := w.ranging.push(v)
	for i := f.base; i < f.end; i++ {
		// Indexed afresh: a nested map can move the stack while stacking its own.
		e := w.ranging.entries[i]
		w.step(pathStep{kind: stepMapKey, form: e.form, n: e.num, label: e.label}, e.key, depth+1, active)
		w.step(pathStep{kind: stepMapValue, form: e.form, n: e.num, label: e.label}, e.value, depth+1, active)
	}
	w.ranging.pop(f)
}
