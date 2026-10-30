// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import "reflect"

// entryReader reads the entries of maps by reflection into a key and a value it
// keeps per map type, where reflect.MapIter.Key and Value copy each entry into
// an allocation of its own. A check keeps one for its walk, so ranging over a
// map allocates nothing once its type has been met. The zero entryReader is
// ready to use.
//
// A caller ranges with MapRange and reads each entry through what heldFor
// returns: no closure stands between them, so nothing escapes however the
// build inlines.
type entryReader struct {
	held map[reflect.Type]heldEntry
}

// heldEntry is where an entryReader copies a map's entries: the key and value
// it keeps for the map's type, or, for a map reached through an unexported
// field, which reflect.Value.SetIterKey refuses to read, an allocation of each
// entry's own.
type heldEntry struct {
	key, value reflect.Value
	apart      bool
}

// heldFor returns where r copies the entries of map m, making the key and value
// it keeps for m's type the first time that type is met.
func (r *entryReader) heldFor(m reflect.Value) heldEntry {
	if !m.CanInterface() {
		return heldEntry{apart: true}
	}
	t := m.Type()
	if h, kept := r.held[t]; kept {
		return h
	}
	if r.held == nil {
		r.held = map[reflect.Type]heldEntry{}
	}
	h := heldEntry{key: reflect.New(t.Key()).Elem(), value: reflect.New(t.Elem()).Elem()}
	r.held[t] = h
	return h
}

// read copies iter's current entry and returns its key and value, which the
// next entry read overwrites, so a caller keeps what it reads from them.
func (h heldEntry) read(iter *reflect.MapIter) (key, value reflect.Value) {
	if h.apart {
		return iter.Key(), iter.Value()
	}
	h.key.SetIterKey(iter)
	h.value.SetIterValue(iter)
	return h.key, h.value
}
