// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// readKey names one reading of a type: what an instance of it holds at the
// property name where object is set, and otherwise what a value of it admits.
type readKey struct {
	id     ir.TypeID
	name   string
	object bool
}

// tagReadingsKey keys a run's tagReadings (lowering.Remembered).
type tagReadingsKey struct{}

// tagReadings remembers what the policy reads of a settled registry, so a type
// many settlements ask about is read once a run. What it reads, a type's
// values, a union's variants and closure, a model's members and composition,
// no settlement rewrites. One swapping a model for its map swaps one holding
// no member and composing nothing, which admits any value either way, so a
// reading taken before the swap answers as one taken after.
type tagReadings struct {
	read map[readKey]tagSet
}

// readingsOf returns the run's tagReadings. A settlement reads the registry
// once every lowering has returned, so every copy of c shares one.
func readingsOf(c lowering.Ctx) *tagReadings {
	return lowering.Remembered(c, tagReadingsKey{}, func() *tagReadings {
		return &tagReadings{read: map[readKey]tagSet{}}
	})
}

// object returns what an instance of the object flat flattens holds at the
// property name: what its member admits (member, or any value where ok is
// false, as it declares none), met with what each union it composes admits
// there (conjunctKeys).
func (r *tagReadings) object(reg ir.TypeRegistry, flat ir.Flattening, member ir.FlatProperty, ok bool, name string) tagSet {
	got := anything()
	if ok {
		got = r.member(reg, member)
	}
	for _, k := range conjunctKeys(reg, flat, name) {
		got = meet(got, r.fold(reg, k))
	}
	return got
}

// member returns what every declaration of p admits, as an instance meets
// them all: the one it takes and those it overrides.
func (r *tagReadings) member(reg ir.TypeRegistry, p ir.FlatProperty) tagSet {
	for _, k := range memberKeys(reg, p) {
		r.fold(reg, k)
	}
	return r.memberOf(reg, p)
}

// fold reads root and each reading it reaches not read yet, once a run, on
// stacks of its own rather than by recursion: Tarjan's strongly connected
// components, each read once the components it reaches are (readComponent),
// so a cycle reads alike from wherever it is entered. Each reading is entered
// once, which bounds the walk.
func (r *tagReadings) fold(reg ir.TypeRegistry, root readKey) tagSet {
	if got, read := r.read[root]; read {
		return got
	}
	w := componentWalk{index: map[readKey]int{}, low: map[readKey]int{}, opened: map[readKey]readFrame{}}
	w.enter(reg, root)
	for len(w.path) > 0 {
		f := &w.path[len(w.path)-1]
		if f.next < len(f.kids) {
			kid := f.kids[f.next]
			f.next++
			if _, read := r.read[kid]; read {
				continue
			}
			if _, seen := w.index[kid]; !seen {
				w.enter(reg, kid)
				continue
			}
			w.low[f.key] = min(w.low[f.key], w.index[kid]) // on the stack, as no component holding it is read
			continue
		}
		done := w.leave()
		if w.low[done.key] == w.index[done.key] {
			r.readComponent(reg, &w, done.key)
		}
	}
	return r.read[root]
}

// maxComponentSweeps bounds the sweeps readComponent makes of a component of
// object readings (styleguide bounded-everything rule). A cycle of unions and
// the objects composing them, as a union listing subtypes that compose it, is
// read in a few.
const maxComponentSweeps = 64

// readComponent reads the component whose first reading entered is root, off
// the walk's stack. Value readings name only value readings, so a component
// holds one kind. A union on a cycle of unions bounds nothing, as a value
// another member admits is taken as any. Object readings meet and join, which
// keep their order, so each sweep reads every member off the sweep before,
// from any value down: each is sound, the last is the answer, and none turns
// on the order of the members.
func (r *tagReadings) readComponent(reg ir.TypeRegistry, w *componentWalk, root readKey) {
	i := len(w.stack) - 1
	for w.stack[i] != root {
		i--
	}
	component := w.stack[i:]
	if root.object {
		r.sweep(reg, w, component)
	} else {
		cycle := make(map[readKey]bool, len(component))
		for _, k := range component {
			cycle[k] = true
		}
		for _, k := range component {
			r.read[k] = r.combine(reg, w.opened[k], cycle)
		}
	}
	for _, k := range component {
		delete(w.opened, k)
	}
	w.stack = w.stack[:i]
}

// sweep reads each member of the component of object readings off what the
// sweep before read, the first off any value, until a sweep changes none or
// maxComponentSweeps are made.
func (r *tagReadings) sweep(reg ir.TypeRegistry, w *componentWalk, component []readKey) {
	for _, k := range component {
		r.read[k] = anything()
	}
	next := make([]tagSet, len(component))
	for range maxComponentSweeps {
		for i, k := range component {
			next[i] = r.combine(reg, w.opened[k], nil)
		}
		changed := false
		for i, k := range component {
			if !sameSet(next[i], r.read[k]) {
				r.read[k], changed = next[i], true
			}
		}
		if !changed {
			return
		}
	}
}

// sameSet reports whether a and b admit the same.
func sameSet(a, b tagSet) bool {
	return a.closed == b.closed && a.null == b.null && slices.Equal(a.strings, b.strings)
}

// combine returns what the reading f opened admits, once each reading it
// names is read, or is in its component and read as far as readComponent
// has. A value reading naming a union in cycle, its component, bounds nothing.
func (r *tagReadings) combine(reg ir.TypeRegistry, f readFrame, cycle map[readKey]bool) tagSet {
	if !f.key.object {
		u := unionNode(reg, f.key.id)
		sets := make([]tagSet, len(u.Variants))
		for i, v := range u.Variants {
			if id, _ := throughAliases(reg, v.Type.Target); cycle[readKey{id: id}] {
				return anything() // a union on a cycle of unions bounds nothing
			}
			sets[i] = r.refOf(reg, v.Type)
		}
		return closedJoin(u, sets, u.Exclusive)
	}
	if u := unionNode(reg, f.key.id); u != nil {
		sets := make([]tagSet, len(u.Variants))
		for i, v := range u.Variants {
			sets[i] = anything()
			if k, ok := instanceKey(reg, v.Type.Target, f.key.name); ok {
				sets[i] = r.read[k]
			}
		}
		// An instance two variants admit by its tag may still match one alone,
		// by what else it holds, so an object reading joins as an anyOf does.
		return closedJoin(u, sets, false /* exclusive */)
	}
	got := anything() // a model's, as no object reading keys another node
	if p, ok := flatMember(f.flat, f.key.name); ok {
		got = r.memberOf(reg, p)
	}
	for _, k := range conjunctKeys(reg, f.flat, f.key.name) {
		got = meet(got, r.read[k])
	}
	return got
}

// closedJoin is join over the variants of u, or any value for a union not
// closed.
func closedJoin(u *ir.Union, sets []tagSet, exclusive bool) tagSet {
	if !u.Closed {
		return anything()
	}
	return join(sets, exclusive)
}

// refOf returns what a value t names admits, read through alias scalars, with
// the null t or an alias it is read through admits: what the value reading of
// a union holds, which fold must have read, and leaf's for any other node,
// read once a run, as many declarations name one Enum.
func (r *tagReadings) refOf(reg ir.TypeRegistry, t ir.TypeRef) tagSet {
	id, null := throughAliases(reg, t.Target)
	k := readKey{id: id}
	got, read := r.read[k]
	if !read { // no union, which fold reads first
		got = leaf(reg[id])
		r.read[k] = got
	}
	got.null = got.null || null || t.Nullable
	return got
}

// memberOf returns what every declaration of p admits, each as refOf reads it.
func (r *tagReadings) memberOf(reg ir.TypeRegistry, p ir.FlatProperty) tagSet {
	got := r.refOf(reg, p.Property.Type)
	for _, d := range p.Overridden {
		got = meet(got, r.refOf(reg, d.Property.Type))
	}
	return got
}

// componentWalk is fold's state: Tarjan's index and low link of each reading
// entered, its stack, the path of readings being entered, and what each
// reading entered and not yet read named.
type componentWalk struct {
	index, low map[readKey]int
	stack      []readKey
	path       []readFrame
	opened     map[readKey]readFrame
}

// readFrame is a reading entered: the readings it names, how many of them the
// walk has visited, and, for an object reading of a model, its flattening.
type readFrame struct {
	key  readKey
	kids []readKey
	next int
	flat ir.Flattening
}

// enter numbers k, stacks it, and puts what it names on the path.
func (w *componentWalk) enter(reg ir.TypeRegistry, k readKey) {
	w.index[k], w.low[k] = len(w.index), len(w.index)
	w.stack = append(w.stack, k)
	w.path = append(w.path, readFrame{key: k, kids: nil})
	f := &w.path[len(w.path)-1]
	f.kids, f.flat = children(reg, k)
}

// leave takes the reading every kid of which is visited off the path, keeps
// what it named for its component, and passes its low link to its parent.
func (w *componentWalk) leave() readFrame {
	done := w.path[len(w.path)-1]
	w.path = w.path[:len(w.path)-1]
	w.opened[done.key] = done
	if len(w.path) > 0 {
		parent := w.path[len(w.path)-1].key
		w.low[parent] = min(w.low[parent], w.low[done.key])
	}
	return done
}

// children returns the readings k names, and a model's flattening for an
// object reading of one: a union's variants that are unions, for a value
// reading; and for an object reading, a model's member's declarations that are
// unions and the closed unions it composes, or a union's variants that are
// models or unions.
func children(reg ir.TypeRegistry, k readKey) ([]readKey, ir.Flattening) {
	var kids []readKey
	if u := unionNode(reg, k.id); u != nil {
		for _, v := range u.Variants {
			if !k.object {
				if id, _ := throughAliases(reg, v.Type.Target); unionNode(reg, id) != nil {
					kids = append(kids, readKey{id: id})
				}
			} else if kid, ok := instanceKey(reg, v.Type.Target, k.name); ok {
				kids = append(kids, kid)
			}
		}
		return kids, ir.Flattening{}
	}
	flat := ir.FlattenedProperties(reg, k.id) // a model's, as no other reading keys another node
	if p, ok := flatMember(flat, k.name); ok {
		kids = memberKeys(reg, p)
	}
	return append(kids, conjunctKeys(reg, flat, k.name)...), flat
}

// memberKeys returns the value readings of the unions p's declarations name.
func memberKeys(reg ir.TypeRegistry, p ir.FlatProperty) []readKey {
	var keys []readKey
	for _, t := range append([]ir.TypeRef{p.Property.Type}, declTypes(p.Overridden)...) {
		if id, _ := throughAliases(reg, t.Target); unionNode(reg, id) != nil {
			keys = append(keys, readKey{id: id})
		}
	}
	return keys
}

// declTypes returns the type each of decls declares.
func declTypes(decls []ir.PropertyDecl) []ir.TypeRef {
	types := make([]ir.TypeRef, len(decls))
	for i, d := range decls {
		types[i] = d.Property.Type
	}
	return types
}

// conjunctKeys returns the object readings at the property name of the unions
// flat's composition names, read through alias scalars. One not closed admits
// any value (closedJoin), which bounds nothing it is met with.
func conjunctKeys(reg ir.TypeRegistry, flat ir.Flattening, name string) []readKey {
	var keys []readKey
	for _, id := range flat.Unflattened {
		target, _ := throughAliases(reg, id)
		if unionNode(reg, target) != nil {
			keys = append(keys, readKey{id: target, name: name, object: true})
		}
	}
	return keys
}

// instanceKey returns the object reading at the property name of the type id
// stands for, read through alias scalars, where it is a model or a union,
// which an instance of an object can be.
func instanceKey(reg ir.TypeRegistry, id ir.TypeID, name string) (readKey, bool) {
	target, _ := throughAliases(reg, id)
	td := reg[target]
	return readKey{id: target, name: name, object: true}, isKind(td, ir.KindModel) || isKind(td, ir.KindUnion)
}
