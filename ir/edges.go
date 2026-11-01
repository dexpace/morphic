// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import (
	"iter"
	"maps"
	"slices"
)

// TypeEdges returns the type IDs td refers to, in the order [WalkValues]
// reaches them, apart from td's own ID. A self-reference counts and an empty
// target does not. It yields bare IDs, inventing no Nullable bit, and covers
// positions that are not TypeRef fields: discriminator targets, a value's ref
// and constructor types. A [DocRef] is no edge: prose naming a type holds nothing
// of it. A nil or typed-nil td yields nothing.
//
// A value nested past [MaxWalkDepth] levels is not entered; the reflection
// walk is cut sooner, and pass.Validate reports that. A pointer two fields
// share is followed from each, unlike in WalkValues, so aliasing costs the
// unfolded size (#859); a cycle is cut.
func TypeEdges(td TypeDef) iter.Seq[TypeID] {
	return func(yield func(TypeID) bool) {
		if IsNilTypeDef(td) {
			return
		}
		w := edgeWalk{yield: yield}
		w.def(td)
	}
}

// edgeWalk is one TypeEdges call: the consumer's yield, whether it has
// declined more, and the values it is inside. Every edge leaves through id, so the rule that yield is never
// called again after it returns false is kept in one place. Once stopped, the
// walk finishes its pass over the type definition without yielding.
//
// Its methods follow field declaration order, which is the order [WalkValues]
// reaches the same fields in.
type edgeWalk struct {
	yield   func(TypeID) bool
	stopped bool
	path    descent
}

// id yields id unless it is empty or the consumer has stopped.
func (w *edgeWalk) id(id TypeID) {
	if w.stopped || id == "" {
		return
	}
	w.stopped = !w.yield(id)
}

// ref yields the target of r, if there is a reference at all.
func (w *edgeWalk) ref(r *TypeRef) {
	if r != nil {
		w.id(r.Target)
	}
}

// refs yields the target of each reference in rs.
func (w *edgeWalk) refs(rs []TypeRef) {
	for i := range rs {
		w.id(rs[i].Target)
	}
}

// def yields td's edges: the part common to every kind, then the kind's own
// fields.
func (w *edgeWalk) def(td TypeDef) {
	w.common(td.Common())
	switch t := td.(type) {
	case *Scalar:
		w.ref(t.Base)
		w.encoding(t.Encoding)
	case *Model:
		w.model(t)
	case *Union:
		w.union(t)
	case *Enum:
		w.enum(t)
	case *List:
		w.ref(&t.Elem)
		w.encoding(t.Encoding)
	case *MapT:
		w.ref(t.Key)
		w.ref(&t.Value)
	case *Tuple:
		w.refs(t.Elems)
		w.ref(t.Rest)
	case *Literal:
		w.value(&t.Value, 0)
	default:
		// Primitive, External and Any hold nothing beyond the common part.
	}
}

// common yields what every kind can hold: the availability timeline, examples
// and the template instantiation.
func (w *edgeWalk) common(c *TypeCommon) {
	w.availability(c.Availability)
	w.examples(c.Examples)
	w.instantiation(c.Instantiation)
}

// value yields the types v refers to, through every nested list, object member
// and constructor argument. depth counts one level per step down; the
// reflection walk charges each of those steps at least twice, so it is cut
// first. A list, object or constructor met again from inside itself is not
// entered again.
func (w *edgeWalk) value(v *Value, depth int) {
	if v == nil || depth > MaxWalkDepth {
		return
	}
	w.values(v.List, depth+1)
	w.members(v.Object, depth+1)
	if v.Ref != nil {
		w.id(v.Ref.Type)
	}
	if c := v.Ctor; c != nil && w.path.enter(c, 0) {
		w.id(c.Scalar)
		w.values(c.Args, depth+1)
		w.path.leave()
	}
}

// values yields the edges of each value in vs, all at depth.
func (w *edgeWalk) values(vs []Value, depth int) {
	if len(vs) == 0 || !w.path.enter(&vs[0], len(vs)) {
		return
	}
	for i := range vs {
		w.value(&vs[i], depth)
	}
	w.path.leave()
}

// members yields the edges of each member of an object, all at depth.
func (w *edgeWalk) members(fs []Field, depth int) {
	if len(fs) == 0 || !w.path.enter(&fs[0], len(fs)) {
		return
	}
	for i := range fs {
		w.value(&fs[i].Value, depth)
	}
	w.path.leave()
}

// examples yields the types each example's values and error refer to.
func (w *edgeWalk) examples(exs []Example) {
	for i := range exs {
		ex := &exs[i]
		w.value(ex.Value, 0)
		w.value(ex.Headers, 0)
		w.value(ex.Input, 0)
		w.value(ex.Output, 0)
		if e := ex.Error; e != nil {
			w.ref(&e.Type)
			w.value(&e.Content, 0)
		}
	}
}

// availability yields the prior types a versioning timeline records.
func (w *edgeWalk) availability(a *Availability) {
	if a == nil {
		return
	}
	for i := range a.TypeChangedFrom {
		w.ref(&a.TypeChangedFrom[i].Type)
	}
}

// encoding yields an encoding's wire type and decoded-schema type.
func (w *edgeWalk) encoding(e *Encoding) {
	if e == nil {
		return
	}
	w.ref(e.WireType)
	w.ref(e.Schema)
}

// instantiation yields the type and value arguments of a monomorphized
// template.
func (w *edgeWalk) instantiation(inst *TemplateInstantiation) {
	if inst == nil {
		return
	}
	for i := range inst.Args {
		w.ref(inst.Args[i].Type)
		w.value(inst.Args[i].Value, 0)
	}
}

// model yields a model's properties, composition parents, catch-all and
// discriminator.
func (w *edgeWalk) model(m *Model) {
	for i := range m.Properties {
		w.property(&m.Properties[i])
	}
	w.ref(m.Base)
	w.refs(m.Implements)
	w.refs(m.Mixins)
	w.additional(m.AdditionalProps)
	w.discriminator(m.Discriminator)
}

// property yields everything a property refers to, its field arguments
// included.
func (w *edgeWalk) property(p *Property) {
	w.ref(&p.Type)
	w.value(p.Default, 0)
	w.encoding(p.Encoding)
	for i := range p.Args {
		w.parameter(&p.Args[i])
	}
	w.examples(p.Examples)
	w.availability(p.Availability)
}

// parameter yields everything a field argument refers to.
func (w *edgeWalk) parameter(p *Parameter) {
	w.ref(&p.Type)
	w.value(p.Default, 0)
	if p.ValueFrom != nil {
		w.ref(p.ValueFrom.Root)
	}
	w.availability(p.Availability)
	w.examples(p.Examples)
}

// additional yields the catch-all value and key types and each pattern's value
// type.
func (w *edgeWalk) additional(a *AdditionalProps) {
	if a == nil {
		return
	}
	w.ref(&a.Value)
	w.ref(a.Key)
	for i := range a.Patterns {
		w.ref(&a.Patterns[i].Value)
	}
}

// discriminator yields the mapping targets in sorted key order, then the
// default target.
func (w *edgeWalk) discriminator(d *Discriminator) {
	if d == nil {
		return
	}
	for _, k := range slices.Sorted(maps.Keys(d.Mapping)) {
		w.id(d.Mapping[k])
	}
	w.id(d.Default)
}

// union yields each variant's references, then the discriminator's.
func (w *edgeWalk) union(u *Union) {
	for i := range u.Variants {
		v := &u.Variants[i]
		w.ref(&v.Type)
		w.availability(v.Availability)
		w.examples(v.Examples)
	}
	w.discriminator(u.Discriminator)
}

// enum yields each member's value, availability and example references.
func (w *edgeWalk) enum(e *Enum) {
	for i := range e.Members {
		m := &e.Members[i]
		w.value(&m.Value, 0)
		w.availability(m.Availability)
		w.examples(m.Examples)
	}
}
