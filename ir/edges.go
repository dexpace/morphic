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
// discriminator targets and a value's ref and constructor types, but no
// [DocRef] or [EntityRef]. A nil or typed-nil td yields nothing.
//
// A pointer two fields share is followed from each, as in WalkValues, and a
// cycle is cut. It enters values no deeper than [MaxWalkDepth] and no more than
// [MaxWalkSteps], so it is cut only where the reflection walk is.
func TypeEdges(td TypeDef) iter.Seq[TypeID] {
	return typeEdges(MaxWalkSteps, td)
}

// typeEdges is TypeEdges entering at most limit values.
func typeEdges(limit int, td TypeDef) iter.Seq[TypeID] {
	return func(yield func(TypeID) bool) {
		if IsNilTypeDef(td) {
			return
		}
		edgeWalkValue := edgeWalk{yield: yield, steps: stepLimit{left: limit}}
		edgeWalkValue.def(td)
	}
}

// edgeWalk is one TypeEdges call: the consumer's yield, whether it has
// declined more, the values it is inside and the steps it may still take.
// Every edge leaves through id, so the rule that yield is never called again
// after it returns false is kept in one place. Once stopped, the walk finishes
// its pass over the type definition without yielding.
//
// Its methods follow field declaration order, which is the order [WalkValues]
// reaches the same fields in.
type edgeWalk struct {
	yield   func(TypeID) bool
	path    descent
	steps   stepLimit
	stopped bool
}

// id yields id unless it is empty or the consumer has stopped.
func (w *edgeWalk) id(id TypeID) {
	if w.stopped || id == "" {
		return
	}
	w.stopped = !w.yield(id)
}

// ref yields the target of typeRef, if there is a reference at all.
func (w *edgeWalk) ref(typeRef *TypeRef) {
	if typeRef != nil {
		w.id(typeRef.Target)
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
		w.ref(&t.ElementType)
		w.encoding(t.Encoding)
	case *MapT:
		w.ref(t.Key)
		w.ref(&t.Value)
	case *Tuple:
		w.refs(t.ElementTypes)
		w.ref(t.Rest)
	case *Literal:
		w.value(&t.Value, 0)
	default:
		// Primitive, External and Any hold nothing beyond the common part.
	}
}

// common yields what every kind can hold: the availability timeline, examples,
// the template instantiation and kept unmodeled targets.
func (w *edgeWalk) common(typeCommon *TypeCommon) {
	w.availability(typeCommon.Availability)
	w.examples(typeCommon.Examples)
	w.instantiation(typeCommon.Instantiation)
	w.unmodeled(typeCommon.Unmodeled)
}

// unmodeled yields each entry's Targets in sorted key order, matching
// WalkValues.
func (w *edgeWalk) unmodeled(unmodeled Unmodeled) {
	if len(unmodeled) == 0 {
		return
	}
	for _, key := range slices.Sorted(maps.Keys(unmodeled)) {
		for _, id := range unmodeled[key].Targets {
			w.id(id)
		}
	}
}

// value yields the types value refers to, through every nested list, object
// member and constructor argument, taking a step for value. depth counts one
// level per step down; the reflection walk charges each of those steps at least
// twice, so it is cut first. A list, object or constructor met again from
// inside itself is not entered again.
func (w *edgeWalk) value(value *Value, depth int) {
	if value == nil || depth > MaxWalkDepth || !w.steps.take() {
		return
	}
	w.values(value.List, depth+1)
	w.members(value.Object, depth+1)
	if value.Ref != nil {
		w.id(value.Ref.Type)
	}
	if constructorValue := value.Constructor; constructorValue != nil && w.path.enter(constructorValue, 0) {
		w.id(constructorValue.Scalar)
		w.values(constructorValue.Args, depth+1)
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
		w.input(ex.Input)
		w.value(ex.Output, 0)
		if errorExample := ex.Error; errorExample != nil {
			w.ref(&errorExample.Type)
			w.value(&errorExample.Content, 0)
		}
		w.unmodeled(ex.Unmodeled)
	}
}

// input yields the types a scenario's input refers to: each parameter's value
// in key order, which is the order the reflection walk ranges the map in, then
// the body's.
func (w *edgeWalk) input(in *ScenarioInput) {
	if in == nil {
		return
	}
	for _, id := range slices.Sorted(maps.Keys(in.Params)) {
		value := in.Params[id]
		w.value(&value, 0)
	}
	w.value(in.Body, 0)
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
func (w *edgeWalk) encoding(encoding *Encoding) {
	if encoding == nil {
		return
	}
	w.ref(encoding.WireType)
	w.ref(encoding.Schema)
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
func (w *edgeWalk) model(model *Model) {
	for i := range model.Properties {
		w.property(&model.Properties[i])
	}
	w.ref(model.Base)
	w.refs(model.Implements)
	w.refs(model.Mixins)
	w.additional(model.AdditionalProperties)
	w.discriminator(model.Discriminator)
}

// property yields everything a property refers to, its field arguments
// included.
func (w *edgeWalk) property(property *Property) {
	w.ref(&property.Type)
	w.value(property.Default, 0)
	w.encoding(property.Encoding)
	for i := range property.Args {
		w.parameter(&property.Args[i])
	}
	w.examples(property.Examples)
	w.availability(property.Availability)
	w.unmodeled(property.Unmodeled)
}

// parameter yields everything a field argument refers to.
func (w *edgeWalk) parameter(parameter *Parameter) {
	w.ref(&parameter.Type)
	w.value(parameter.Default, 0)
	w.encoding(parameter.Encoding)
	if parameter.ValueFrom != nil {
		w.ref(parameter.ValueFrom.Root)
	}
	w.availability(parameter.Availability)
	w.examples(parameter.Examples)
	w.unmodeled(parameter.Unmodeled)
}

// additional yields the catch-all value and key types and each pattern's value
// type.
func (w *edgeWalk) additional(a *AdditionalProperties) {
	if a == nil {
		return
	}
	w.ref(a.Value)
	w.ref(a.Key)
	for i := range a.Patterns {
		w.ref(&a.Patterns[i].Value)
	}
}

// discriminator yields the mapping targets in sorted key order, then the
// default target.
func (w *edgeWalk) discriminator(discriminator *Discriminator) {
	if discriminator == nil {
		return
	}
	for _, k := range slices.Sorted(maps.Keys(discriminator.Mapping)) {
		w.id(discriminator.Mapping[k])
	}
	w.id(discriminator.Default)
}

// union yields each variant's references, then the discriminator's.
func (w *edgeWalk) union(union *Union) {
	for i := range union.Variants {
		variant := &union.Variants[i]
		w.ref(&variant.Type)
		w.availability(variant.Availability)
		w.examples(variant.Examples)
		w.unmodeled(variant.Unmodeled)
	}
	w.discriminator(union.Discriminator)
}

// enum yields each member's value, availability, example and unmodeled references.
func (w *edgeWalk) enum(enum *Enum) {
	for i := range enum.Members {
		enumMember := &enum.Members[i]
		w.value(&enumMember.Value, 0)
		w.availability(enumMember.Availability)
		w.examples(enumMember.Examples)
		w.unmodeled(enumMember.Unmodeled)
	}
}
