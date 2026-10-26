package ir

import (
	"fmt"
	"math/bits"
	"reflect"
	"slices"
	"strconv"
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

// Visitor is shown each value a [WalkValues] traversal reaches.
type Visitor interface {
	// Visit is called on v, reached by the path at, and reports whether this
	// visitor is to be shown what v holds; false prunes v's children for it
	// alone.
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
// finite encoding.
type CycleVisitor interface {
	Visitor
	// Cycle is called at at, where the walk meets again a reference of type ref
	// it entered at entered, an ancestor of at. The walk does not enter it again.
	Cycle(at, entered WalkPath, ref reflect.Type)
}

// WalkValues walks root by reflection, bounded and cycle-guarded, showing every
// value to each visitor, and reports whether the depth cap truncated the walk.
//
// The visitors share one traversal, yet each sees exactly the walk it would see
// alone, its own pruning and where it first meets a shared pointer included.
// They are called in the order given; a nil one is skipped.
//
// Reflection covers a new IR field at once. Map keys are reached too, since
// some are references; entries come in rendered-key order, and byte elements
// are skipped. Interface() panics behind an unexported field, so visitors read
// fields.
func WalkValues(root any, path string, visitors ...Visitor) bool {
	v := reflect.ValueOf(root)
	truncated := false
	for group := range slices.Chunk(visitors, maxSharedVisitors) {
		w := newValueWalk(path, group)
		w.walk(v, 0, w.all)
		truncated = truncated || w.truncated
	}
	return truncated
}

// maxSharedVisitors is how many visitors one traversal carries: the width of
// [visitorSet]. [WalkValues] gives further visitors traversals of their own,
// which none can tell from a shared one.
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
// It is valid only until the call it was passed to returns; a visitor that keeps
// a path keeps its String. The zero WalkPath is empty.
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
// owner and index, an element by its index, an entry by its rendered key.
type pathStep struct {
	kind  stepKind
	owner reflect.Type
	index int
	label string
}

// appendTo renders the step onto b. A field read through an embedded struct is
// promoted by Go and inlined by JSON, so the embedded field itself renders
// nothing: "….TypeCommon.Examples[0]" would name a step neither encoding has.
func (s pathStep) appendTo(b []byte) []byte {
	switch s.kind {
	case stepField:
		if f := s.owner.Field(s.index); !f.Anonymous {
			b = append(append(b, '.'), f.Name...)
		}
	case stepElement:
		b = append(strconv.AppendInt(append(b, '['), int64(s.index), 10), ']')
	case stepMapKey:
		b = append(append(append(append(b, '['), s.label...), ']'), MapKeySuffix...)
	case stepMapValue:
		b = append(append(append(b, '['), s.label...), ']')
	}
	return b
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

// valueWalk is one traversal's state: its visitors, which pointers each has
// entered, the references and steps the walk is inside right now, the map
// entries it is ranging over, and whether the depth cap cut it short.
//
// It is a value being walked rather than a walker being configured, so it is
// built at the entry point and discarded with it. Nothing here is reentrant and
// nothing outside this file holds one.
type valueWalk struct {
	root      string
	visitors  []Visitor
	cycles    []CycleVisitor
	all       visitorSet
	seen      map[refKey]visitorSet
	inside    []insideRef
	steps     []pathStep
	ranging   []mapEntry
	truncated bool
}

// newValueWalk builds the state of one traversal from root showing visitors,
// of which there are at most maxSharedVisitors. The step stack is sized up front
// so that a walk deeper than a struct or two costs no allocation per level.
func newValueWalk(root string, visitors []Visitor) *valueWalk {
	w := &valueWalk{
		root:     root,
		visitors: visitors,
		cycles:   make([]CycleVisitor, len(visitors)),
		seen:     map[refKey]visitorSet{},
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
// those that ask. The invalid zero Value — a nil root, a nil interface's dynamic
// value — is not shown, so no visitor has to guard against calling Type() on
// one.
func (w *valueWalk) walk(v reflect.Value, depth int, active visitorSet) {
	if !v.IsValid() {
		return
	}
	at := w.here()
	var asking visitorSet
	for set := active; set != 0; set &= set - 1 {
		i := bits.TrailingZeros64(uint64(set))
		if w.visitors[i].Visit(v, at) {
			asking |= 1 << i
		}
	}
	if asking != 0 {
		w.children(v, depth, asking)
	}
}

// step descends into child, reached from its parent by s.
func (w *valueWalk) step(s pathStep, child reflect.Value, depth int, active visitorSet) {
	w.steps = append(w.steps, s)
	w.descend(child, depth, active)
	w.steps = w.steps[:len(w.steps)-1]
}

// descend continues into a child unless that would pass the depth cap, which it
// records rather than reports: the caller decides whether a truncated walk is a
// finding, and it is the only one that knows what was being checked.
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
			w.step(pathStep{kind: stepField, owner: t, index: i}, v.Field(i), depth+1, active)
		}
	case reflect.Slice, reflect.Array:
		w.sequence(v, depth, active)
	case reflect.Map:
		w.mapping(v, depth, active)
	}
}

// pointer descends through a non-nil pointer once per visitor, so a value
// reached through a shared pointer is shown to each visitor once, at the first
// path that visitor reaches it by. One met again while the walk is still inside
// it closes a cycle, which closesCycle reports.
//
// Which visitors have entered a pointer is recorded per pointer, so one
// visitor's pruning never decides where another meets a shared value. Which
// referrer wins is decided by traversal order, so the walk's order has to be
// fixed rather than incidental; appendOrderedEntries is where that is arranged.
func (w *valueWalk) pointer(v reflect.Value, depth int, active visitorSet) {
	if v.IsNil() {
		return
	}
	key := refKey{typ: v.Type(), ptr: v.Pointer()}
	entered := w.seen[key]
	fresh := active &^ entered
	if fresh == 0 {
		w.closesCycle(key, active)
		return
	}
	w.seen[key] = entered | fresh
	w.enter(key)
	w.descend(v.Elem(), depth+1, fresh)
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
// Every visitor in active entered key where the walk did, since the walk went on
// into key only for those that had not entered it before.
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
		w.step(pathStep{kind: stepElement, index: i}, v.Index(i), depth+1, active)
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
// stacks its own above them and takes them off again, so ranging over a map
// allocates nothing once the stack has grown to the walk's widest.
func (w *valueWalk) entries(v reflect.Value, depth int, active visitorSet) {
	base := len(w.ranging)
	w.ranging = appendOrderedEntries(w.ranging, v)
	for i := base; i < len(w.ranging); i++ {
		// Indexed afresh: a nested map can move the stack while stacking its own.
		e := w.ranging[i]
		w.step(pathStep{kind: stepMapKey, label: e.label}, e.key, depth+1, active)
		w.step(pathStep{kind: stepMapValue, label: e.label}, e.value, depth+1, active)
	}
	w.ranging = w.ranging[:base]
}

// mapEntry is one map entry paired with its rendered key, which both spells the
// entry's path and orders the walk.
type mapEntry struct {
	label string
	key   reflect.Value
	value reflect.Value
}

// appendOrderedEntries appends v's entries to dst ordered by rendered key rather
// than Go's randomized map order. A pointer reachable from two entries is
// descended into at whichever the walk reaches first, so a random order would
// change the result set, not just its order, and no later sort repairs that
// (invariant 7). The rendering is the one the path uses, which keeps the two in
// step, and it is a total order for every key type the IR declares: named
// string types, plain strings and ints all render distinct keys distinctly.
func appendOrderedEntries(dst []mapEntry, v reflect.Value) []mapEntry {
	base := len(dst)
	plain := rendersPlainly(v.Type().Key())
	// MapRange rather than v.Seq2(), which puts the loop body on the heap.
	for iter := v.MapRange(); iter.Next(); {
		k := iter.Key()
		dst = append(dst, mapEntry{label: keyLabel(k, plain), key: k, value: iter.Value()})
	}
	slices.SortFunc(dst[base:], func(a, b mapEntry) int { return strings.Compare(a.label, b.label) })
	return dst
}

// The interfaces through which fmt lets a type spell itself under %v.
var (
	formatterType = reflect.TypeFor[fmt.Formatter]()
	stringerType  = reflect.TypeFor[fmt.Stringer]()
	errorType     = reflect.TypeFor[error]()
)

// rendersPlainly reports whether fmt's %v spells a key of type t as its plain
// string or decimal integer: a string or integer kind with none of the methods
// through which fmt lets a type spell itself.
func rendersPlainly(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return !t.Implements(formatterType) && !t.Implements(stringerType) && !t.Implements(errorType)
	default:
		return false
	}
}

// keyLabel renders a map key as fmt's %v does, which is how a path spells it,
// without fmt's two allocations for a key that renders plainly: a string key is
// its own label.
func keyLabel(k reflect.Value, plain bool) string {
	if !plain {
		return fmt.Sprintf("%v", k)
	}
	switch k.Kind() {
	case reflect.String:
		return k.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10)
	default:
		return strconv.FormatUint(k.Uint(), 10)
	}
}
