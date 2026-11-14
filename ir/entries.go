// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// entryStack holds the entries of the maps a traversal ranges over, the
// innermost map's last, each map's in rendered-key order. [WalkValues] and the
// deep copy each range through one, so they meet a map's entries in one order.
//
// Keys and values are copied into storage kept per map type and reused by the
// next map of that type, where reflect.MapIter.Key and Value allocate a copy of
// each, so copying a map's entries allocates nothing once that storage fits; an
// integer key is held as its value, so only a key fmt spells costs its label.
// The zero entryStack is ready to use.
type entryStack struct {
	entries []mapEntry
	stores  map[reflect.Type]*entryStore
}

// mapEntry is one map entry paired with its key as a path spells it, which also
// orders the walk: label for a labelled key, num for an integer one.
type mapEntry struct {
	key   reflect.Value
	value reflect.Value
	label string
	num   uint64
	form  keyForm
}

// keyForm is how an entry or a path step holds a map key: as its label, or as
// an integer rendered in decimal only when a path is spelled or two keys are
// ordered.
type keyForm uint8

const (
	labelledKey keyForm = iota // label holds the key as fmt's %v spells it
	signedKey                  // num holds a signed key's bits
	unsignedKey                // num holds an unsigned key
)

// appendKey renders a key held in form onto b, as fmt's %v spells it.
func appendKey(b []byte, form keyForm, num uint64, label string) []byte {
	switch form {
	case signedKey:
		return strconv.AppendInt(b, int64(num), 10)
	case unsignedKey:
		return strconv.AppendUint(b, num, 10)
	default:
		return append(b, label...)
	}
}

// appendLabel renders e's key onto b, as a path spells it.
func (e mapEntry) appendLabel(b []byte) []byte {
	return appendKey(b, e.form, e.num, e.label)
}

// compareEntries orders two entries of one map by their keys' spellings, byte
// by byte: "10" before "9", and "-1" first. An integer key is rendered for the
// comparison into a buffer on the stack, which holds the longest, 20 bytes.
func compareEntries(a, b mapEntry) int {
	if a.form == labelledKey {
		return strings.Compare(a.label, b.label)
	}
	var x, y [20]byte
	return bytes.Compare(a.appendLabel(x[:0]), b.appendLabel(y[:0]))
}

// entryFrame is one map's entries on an entryStack, entries[base:end], copied
// into store's slots from slot on, or each into its own allocation when store
// is nil.
type entryFrame struct {
	base, end int
	store     *entryStore
	slot      int
}

// push stacks the entries of map v and returns where they sit, valid until the
// frame is popped; a map pushed above holds its own, even one of v's type.
//
// They are ordered by rendered key, not Go's randomized map order: a visitor is
// shown values, and keeps what it finds, in walk order, which a random order
// would change from run to run (invariant 7). The path spells keys alike, and
// distinct keys of every type the IR declares render distinctly. A map behind
// an unexported field is copied entry by entry, as SetIterKey refuses to read
// one.
func (s *entryStack) push(v reflect.Value) entryFrame {
	f := entryFrame{base: len(s.entries)}
	n := v.Len()
	if n > 0 && v.CanInterface() {
		f.store = s.storeFor(v.Type())
		f.slot = f.store.reserve(n)
	}
	form := formOf(v.Type().Key())
	// MapRange rather than v.Seq2(), which puts the loop body on the heap.
	for iter := v.MapRange(); iter.Next(); {
		k, val := f.copyEntry(iter, len(s.entries)-f.base)
		s.entries = append(s.entries, entryOf(k, val, form))
	}
	f.end = len(s.entries)
	slices.SortFunc(s.entries[f.base:], compareEntries)
	return f
}

// entryOf pairs the key k and value val of a map whose keys are held in form.
func entryOf(k, val reflect.Value, form keyForm) mapEntry {
	e := mapEntry{key: k, value: val, form: form}
	switch form {
	case signedKey:
		e.num = uint64(k.Int())
	case unsignedKey:
		e.num = k.Uint()
	default:
		e.label = keyLabel(k)
	}
	return e
}

// pop takes f's entries off the stack, which must hold no frame above f, and
// frees its slots for the next map of its type.
func (s *entryStack) pop(f entryFrame) {
	s.entries = s.entries[:f.base]
	if f.store != nil {
		f.store.used = f.slot
	}
}

// storeFor returns the storage kept for maps of type t, making an empty one the
// first time t is met.
func (s *entryStack) storeFor(t reflect.Type) *entryStore {
	if st, kept := s.stores[t]; kept {
		return st
	}
	if s.stores == nil {
		s.stores = map[reflect.Type]*entryStore{}
	}
	st := &entryStore{
		keys:   reflect.Zero(reflect.SliceOf(t.Key())),
		values: reflect.Zero(reflect.SliceOf(t.Elem())),
	}
	s.stores[t] = st
	return st
}

// copyEntry copies iter's entry, the i-th of f's map, into the slot f keeps for
// it, or into an allocation of its own when f keeps none.
func (f entryFrame) copyEntry(iter *reflect.MapIter, i int) (key, value reflect.Value) {
	if f.store == nil {
		return iter.Key(), iter.Value()
	}
	key, value = f.store.keys.Index(f.slot+i), f.store.values.Index(f.slot+i)
	key.SetIterKey(iter)
	value.SetIterValue(iter)
	return key, value
}

// entryStore holds copies of the keys and values of maps of one type, in slots
// shared by the maps of that type on an entryStack: those maps have claimed
// slots [0, used), outermost first, though what one copied before reserve moved
// to larger slices stays in the old ones.
type entryStore struct {
	// keys and values are slices of the map type's key and element types, each
	// as long as its capacity.
	keys, values reflect.Value
	used         int
}

// reserve claims n slots after those in use and returns the first. When they
// do not fit, it moves to new slices twice the size or more: an entry already
// copied keeps reading the old ones, which nothing writes again.
func (st *entryStore) reserve(n int) int {
	first := st.used
	if size := st.keys.Len(); first+n > size {
		size = max(2*size, first+n)
		st.keys = reflect.MakeSlice(st.keys.Type(), size, size)
		st.values = reflect.MakeSlice(st.values.Type(), size, size)
	}
	st.used = first + n
	return first
}

// The interfaces through which fmt lets a type spell itself under %v.
var (
	formatterType = reflect.TypeFor[fmt.Formatter]()
	stringerType  = reflect.TypeFor[fmt.Stringer]()
	errorType     = reflect.TypeFor[error]()
)

// formOf returns how the keys of a map keyed by t are held: an integer that
// fmt's %v spells as its plain decimal is held as its value, and every other
// key by its label.
func formOf(t reflect.Type) keyForm {
	if !rendersPlainly(t) {
		return labelledKey
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return signedKey
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return unsignedKey
	default:
		return labelledKey
	}
}

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

// keyLabel renders a map key held by its label as fmt's %v does, which is how
// a path spells it, without fmt's two allocations for a string key that
// renders plainly: such a key is its own label.
func keyLabel(k reflect.Value) string {
	if k.Kind() == reflect.String && rendersPlainly(k.Type()) {
		return k.String()
	}
	return fmt.Sprintf("%v", k)
}
