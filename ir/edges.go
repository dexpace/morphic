package ir

import (
	"iter"
	"slices"
)

// idYield receives one edge and reports whether the walk should continue. The
// helpers below stop at the first false and never call it again, which is the
// iter.Seq contract.
type idYield = func(TypeID) bool

// TypeEdges returns the type IDs td refers to, in field declaration order: every
// non-empty TypeID position it holds, apart from its own ID. It yields bare
// IDs, never a [TypeRef], so a reference's Nullable bit is not invented.
//
// Positions that are not TypeRef fields count too — discriminator targets, a
// value's ref and constructor types — because the reflection walk reaches them.
// A nil or typed-nil td yields nothing, and the sequence is not called again
// once the consumer stops.
func TypeEdges(td TypeDef) iter.Seq[TypeID] {
	return func(yield func(TypeID) bool) {
		if IsNilTypeDef(td) {
			return
		}
		typeDefEdges(td, yield)
	}
}

// typeDefEdges yields td's edges: the part common to every kind, then the kind's
// own fields.
func typeDefEdges(td TypeDef, yield idYield) {
	if !commonEdges(yield, td.Common()) {
		return
	}
	switch t := td.(type) {
	case *Scalar:
		scalarEdges(t, yield)
	case *Model:
		modelEdges(t, yield)
	case *Union:
		unionEdges(t, yield)
	case *Enum:
		enumEdges(t, yield)
	case *List:
		_ = refEdge(yield, &t.Elem) && encodingEdges(yield, t.Encoding)
	case *MapT:
		_ = refEdge(yield, &t.Key) && refEdge(yield, &t.Value)
	case *Tuple:
		refsEdges(yield, t.Elems)
	case *Literal:
		_ = valueEdges(yield, &t.Value, 0)
	default:
		// Primitive, External and Any hold nothing beyond the common part.
	}
}

// idEdge yields id unless it is empty.
func idEdge(yield idYield, id TypeID) bool {
	if id == "" {
		return true
	}
	return yield(id)
}

// refEdge yields the target of r, if there is a reference at all.
func refEdge(yield idYield, r *TypeRef) bool {
	if r == nil {
		return true
	}
	return idEdge(yield, r.Target)
}

// refsEdges yields the target of each reference in refs.
func refsEdges(yield idYield, refs []TypeRef) bool {
	for i := range refs {
		if !refEdge(yield, &refs[i]) {
			return false
		}
	}
	return true
}

// valueEdges yields the types a value refers to: a ref's declaring type and a
// constructor's scalar, through every nested list, object member and argument.
// Nesting below MaxWalkDepth is not entered, the same bound the reflection walk
// keeps.
func valueEdges(yield idYield, v *Value, depth int) bool {
	if v == nil || depth > MaxWalkDepth {
		return true
	}
	if v.Ref != nil && !idEdge(yield, v.Ref.Type) {
		return false
	}
	if v.Ctor != nil {
		if !idEdge(yield, v.Ctor.Scalar) || !valuesEdges(yield, v.Ctor.Args, depth+1) {
			return false
		}
	}
	if !valuesEdges(yield, v.List, depth+1) {
		return false
	}
	for i := range v.Object {
		if !valueEdges(yield, &v.Object[i].Value, depth+1) {
			return false
		}
	}
	return true
}

// valuesEdges yields the edges of each value in vs.
func valuesEdges(yield idYield, vs []Value, depth int) bool {
	for i := range vs {
		if !valueEdges(yield, &vs[i], depth) {
			return false
		}
	}
	return true
}

// exampleEdges yields the types each example's values and error refer to.
func exampleEdges(yield idYield, examples []Example) bool {
	for i := range examples {
		ex := &examples[i]
		ok := valueEdges(yield, ex.Value, 0) &&
			valueEdges(yield, ex.Headers, 0) &&
			valueEdges(yield, ex.Input, 0) &&
			valueEdges(yield, ex.Output, 0)
		if !ok {
			return false
		}
		if ex.Error != nil && !errorExampleEdges(yield, ex.Error) {
			return false
		}
	}
	return true
}

// errorExampleEdges yields the error type of a scenario and the content value.
func errorExampleEdges(yield idYield, e *ErrorExample) bool {
	return refEdge(yield, &e.Type) && valueEdges(yield, &e.Content, 0)
}

// availabilityEdges yields the prior types a versioning timeline records.
func availabilityEdges(yield idYield, a *Availability) bool {
	if a == nil {
		return true
	}
	for i := range a.TypeChangedFrom {
		if !refEdge(yield, &a.TypeChangedFrom[i].Type) {
			return false
		}
	}
	return true
}

// encodingEdges yields an encoding's wire type and decoded-schema type.
func encodingEdges(yield idYield, e *Encoding) bool {
	if e == nil {
		return true
	}
	return refEdge(yield, e.WireType) && refEdge(yield, e.Schema)
}

// instantiationEdges yields the type and value arguments of a monomorphized
// template.
func instantiationEdges(yield idYield, inst *TemplateInstantiation) bool {
	if inst == nil {
		return true
	}
	for i := range inst.Args {
		if !refEdge(yield, inst.Args[i].Type) || !valueEdges(yield, inst.Args[i].Value, 0) {
			return false
		}
	}
	return true
}

// commonEdges yields what every kind can hold: examples, the availability
// timeline and the template instantiation.
func commonEdges(yield idYield, c *TypeCommon) bool {
	return availabilityEdges(yield, c.Availability) &&
		exampleEdges(yield, c.Examples) &&
		instantiationEdges(yield, c.Instantiation)
}

// scalarEdges yields a scalar's base and encoding references.
func scalarEdges(s *Scalar, yield idYield) {
	_ = refEdge(yield, s.Base) && encodingEdges(yield, s.Encoding)
}

// modelEdges yields a model's properties, composition parents, catch-all and
// discriminator.
func modelEdges(m *Model, yield idYield) {
	for i := range m.Properties {
		if !propertyEdges(yield, &m.Properties[i]) {
			return
		}
	}
	_ = refEdge(yield, m.Base) &&
		refsEdges(yield, m.Implements) &&
		refsEdges(yield, m.Mixins) &&
		additionalEdges(yield, m.AdditionalProps) &&
		discriminatorEdges(yield, m.Discriminator)
}

// propertyEdges yields everything a property refers to, its field arguments
// included.
func propertyEdges(yield idYield, p *Property) bool {
	ok := refEdge(yield, &p.Type) &&
		valueEdges(yield, p.Default, 0) &&
		encodingEdges(yield, p.Encoding)
	if !ok {
		return false
	}
	for i := range p.Args {
		if !parameterEdges(yield, &p.Args[i]) {
			return false
		}
	}
	return exampleEdges(yield, p.Examples) && availabilityEdges(yield, p.Availability)
}

// parameterEdges yields everything a field argument refers to.
func parameterEdges(yield idYield, p *Parameter) bool {
	return refEdge(yield, &p.Type) &&
		valueEdges(yield, p.Default, 0) &&
		pathRootEdge(yield, p.ValueFrom) &&
		exampleEdges(yield, p.Examples) &&
		availabilityEdges(yield, p.Availability)
}

// pathRootEdge yields the type a property path roots in, when it names one.
func pathRootEdge(yield idYield, p *PropPath) bool {
	if p == nil {
		return true
	}
	return refEdge(yield, p.Root)
}

// additionalEdges yields the catch-all value and key types and each pattern's
// value type.
func additionalEdges(yield idYield, a *AdditionalProps) bool {
	if a == nil {
		return true
	}
	if !refEdge(yield, &a.Value) || !refEdge(yield, a.Key) {
		return false
	}
	for i := range a.Patterns {
		if !refEdge(yield, &a.Patterns[i].Value) {
			return false
		}
	}
	return true
}

// discriminatorEdges yields the mapping targets in sorted key order, then the
// default target.
func discriminatorEdges(yield idYield, d *Discriminator) bool {
	if d == nil {
		return true
	}
	keys := make([]string, 0, len(d.Mapping))
	for k := range d.Mapping {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if !idEdge(yield, d.Mapping[k]) {
			return false
		}
	}
	return idEdge(yield, d.Default)
}

// unionEdges yields each variant's references, then the discriminator's.
func unionEdges(u *Union, yield idYield) {
	for i := range u.Variants {
		v := &u.Variants[i]
		ok := refEdge(yield, &v.Type) &&
			exampleEdges(yield, v.Examples) &&
			availabilityEdges(yield, v.Availability)
		if !ok {
			return
		}
	}
	_ = discriminatorEdges(yield, u.Discriminator)
}

// enumEdges yields each member's value, examples and availability references.
func enumEdges(e *Enum, yield idYield) {
	for i := range e.Members {
		m := &e.Members[i]
		ok := valueEdges(yield, &m.Value, 0) &&
			exampleEdges(yield, m.Examples) &&
			availabilityEdges(yield, m.Availability)
		if !ok {
			return
		}
	}
}
