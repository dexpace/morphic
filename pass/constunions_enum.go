// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"reflect"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// unionValues are the values a union read as an Enum holds, in variant order:
// each a member beside its key, or the values of a union branch of several
// that only this union names, which its Enum reads in place. A branch of
// several values named elsewhere too keeps the union, as an Enum would copy
// the set into each union naming it, which a document can make grow with the
// square of its size.
type unionValues struct {
	union *ir.Union
	prim  ir.PrimKind
	parts []valuePart
	// n is how many values it holds, and keys the key of each, which the one
	// union taking these values takes over (distinct).
	n    int
	keys map[string]bool
}

// valuePart is one value, a member holding what is said of it beside its key,
// or, where of is set, the values of a union branch.
type valuePart struct {
	key    string
	member ir.EnumMember
	of     *unionValues
}

// valuesOf returns the values u holds, where u is a closed union saying
// nothing an Enum cannot (plainUnion) whose every variant names values a
// member can hold (appendValues), no value twice and all of one ValueType, and
// false otherwise. Its members take IDs beneath u, which needs u's namespace
// among spaces, those declared for members. Its values being distinct, an
// instance matches one variant at most, so u's Exclusive says nothing lost.
func (r *unionReading) valuesOf(u *ir.Union) (*unionValues, bool) {
	space, spaced := ir.IDSpace(ir.IDKindType, string(u.ID))
	if len(u.Variants) == 0 || !u.Closed || !plainUnion(*u) || !spaced || !slices.Contains(r.spaces, space) {
		return nil, false
	}
	vs := &unionValues{union: u, parts: make([]valuePart, 0, len(u.Variants))}
	for i, v := range u.Variants {
		parts, prim, ok := r.appendValues(vs.parts, v)
		if !ok || (i > 0 && prim != vs.prim) {
			return nil, false
		}
		vs.parts, vs.prim = parts, prim
	}
	return vs, r.distinct(vs)
}

// plainUnion reports whether u says nothing beside its common part, its
// variants, whether it is closed and whether it is exclusive: no tag, no
// discriminator, no input-only identity, nor a field ir adds later, none of
// which an Enum holds.
func plainUnion(u ir.Union) bool {
	u.TypeCommon, u.Variants, u.Exclusive, u.Closed = ir.TypeCommon{}, nil, false, false
	return reflect.ValueOf(u).IsZero()
}

// appendValues appends to parts the values v names, and returns them with the
// primitive typing them, where v says nothing but what it says of a value
// (variantFacts) and names an anonymous type of strings saying nothing else
// (branchValues, commonFacts). One value takes what v and its type say of it.
// Several are a set of their own, taken only where v is their type's one
// reference and neither says anything of them all.
func (r *unionReading) appendValues(parts []valuePart, v ir.Variant) ([]valuePart, ir.PrimKind, bool) {
	said, plain := variantFacts(v)
	if !plain {
		return parts, "", false
	}
	values, prim, common, ok := r.branchValues(v.Type.Target)
	if !ok {
		return parts, "", false
	}
	typed, plain := commonFacts(common)
	if !plain {
		return parts, "", false
	}
	if len(values) > 1 || values[0].of != nil {
		if !r.refs.sole(v.Type.Target) || !said.none() || !typed.none() {
			return parts, "", false
		}
		return append(parts, values...), prim, true
	}
	said, ok = said.with(typed)
	if !ok {
		return parts, "", false
	}
	one := values[0]
	said, ok = said.with(factsOf(one.member))
	one.member = said.onto(one.member)
	return append(parts, one), prim, ok
}

// branchValues returns the values the type at id holds, its primitive and its
// common part, where it is a string Literal or a closed Enum of strings, typed
// by a primitive and saying nothing else, or a union read as an Enum. A
// Literal's member is named by its value, as an enum's members are; an Enum's
// are copies of its own; a union of one value gives its one member, and one of
// several the values it holds.
func (r *unionReading) branchValues(id ir.TypeID) ([]valuePart, ir.PrimKind, ir.TypeCommon, bool) {
	switch t := r.types[id].(type) {
	case *ir.Literal:
		if t == nil || t.Value.Kind != ir.ValueString || t.ValueType == "" {
			return nil, "", ir.TypeCommon{}, false
		}
		rest := *t
		rest.TypeCommon, rest.Value, rest.ValueType = ir.TypeCommon{}, ir.Value{}, ""
		one := ir.EnumMember{Name: ir.NamingFor(t.Value.Str), Value: t.Value}
		return []valuePart{{key: valueKey(t.Value), member: one}}, t.ValueType, t.TypeCommon, reflect.ValueOf(rest).IsZero()
	case *ir.Enum:
		if t == nil || !t.Closed || len(t.Members) == 0 || t.ValueType == "" ||
			slices.ContainsFunc(t.Members, func(m ir.EnumMember) bool { return m.Value.Kind != ir.ValueString }) {
			return nil, "", ir.TypeCommon{}, false
		}
		rest := *t
		rest.TypeCommon, rest.ValueType, rest.Members, rest.Closed = ir.TypeCommon{}, "", nil, false
		values := make([]valuePart, len(t.Members))
		for i, m := range t.Members {
			values[i] = valuePart{key: valueKey(m.Value), member: m}
		}
		return values, t.ValueType, t.TypeCommon, reflect.ValueOf(rest).IsZero()
	case *ir.Union:
		vs, read := r.values[id]
		switch {
		case !read:
			return nil, "", ir.TypeCommon{}, false
		case vs.n == 1:
			return vs.parts, vs.prim, vs.union.TypeCommon, true
		default:
			return []valuePart{{of: vs}}, vs.prim, vs.union.TypeCommon, true
		}
	default:
		return nil, "", ir.TypeCommon{}, false
	}
}

// valueKey returns the key of the string v (ir.EnumMemberKey), which every
// string has.
func valueKey(v ir.Value) string {
	key, _ := ir.EnumMemberKey(v)
	return key
}

// distinct reports whether vs holds no value twice, and records how many it
// holds and their keys. It takes over the keys of the largest union whose
// values it holds, which no other union takes, and adds the rest, so a key is
// copied only into a set at least twice the size of the one it leaves, and a
// chain of unions each naming the next costs the values it holds, not their
// square.
func (r *unionReading) distinct(vs *unionValues) bool {
	var largest *unionValues
	for _, p := range vs.parts {
		if p.of != nil && (largest == nil || p.of.n > largest.n) {
			largest = p.of
		}
	}
	keys := map[string]bool{}
	if largest != nil {
		keys = largest.keys
	}
	for _, p := range vs.parts {
		if !r.addPart(keys, p, largest) {
			return false
		}
	}
	vs.keys, vs.n = keys, len(keys)
	return true
}

// addPart adds to keys the key of each value p holds, bar those of taken,
// whose keys keys holds already, and reports whether none was there.
func (r *unionReading) addPart(keys map[string]bool, p valuePart, taken *unionValues) bool {
	switch p.of {
	case nil:
		return r.addKey(keys, p.key)
	case taken:
		return true
	}
	for key := range p.of.keys {
		if !r.addKey(keys, key) {
			return false
		}
	}
	return true
}

// addKey adds key to keys and reports whether it was not there.
func (r *unionReading) addKey(keys map[string]bool, key string) bool {
	r.work++
	if keys[key] {
		return false
	}
	keys[key] = true
	return true
}

// enum returns the Enum vs makes of its union: the union's common part, and a
// member for each value in variant order, the values of each union it took
// read in place, identified beneath the union. valuesOf took the union only
// where its ID is well formed, so each member's ID derives.
func (r *unionReading) enum(vs *unionValues) *ir.Enum {
	type frame struct {
		vs   *unionValues
		next int
	}
	members := make([]ir.EnumMember, 0, vs.n)
	stack := []frame{{vs: vs}}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next == len(top.vs.parts) {
			stack = stack[:len(stack)-1]
			continue
		}
		p := top.vs.parts[top.next]
		top.next++
		if p.of != nil {
			stack = append(stack, frame{vs: p.of})
			continue
		}
		r.work++
		m := p.member
		m.ID, _ = ir.EnumMemberIDFor(vs.union.ID, p.key, 1 /* occurrence */)
		members = append(members, m)
	}
	return &ir.Enum{TypeCommon: vs.union.TypeCommon, ValueType: vs.prim, Members: members, Closed: true}
}

// commonFacts returns what c, the common part of a variant's type, says of a
// value, and whether it says nothing else: the type is anonymous, named only by
// the hint its position gives it, keeps no construct but vendor extensions, and
// no heuristic inferred it, whose mark no member could carry. Its provenance
// goes with the type.
func commonFacts(c ir.TypeCommon) (memberFacts, bool) {
	f := memberFacts{docs: c.Docs, deprecation: c.Deprecation, availability: c.Availability,
		examples: c.Examples, unmodeled: c.Unmodeled}
	if !c.Anonymous || !declaresNoName(c.Name) || !extensionsOnly(c.Unmodeled) || c.Provenance.Inferred != "" {
		return f, false
	}
	c.ID, c.Anonymous, c.Name, c.Provenance = "", false, ir.Naming{}, ir.Provenance{}
	c.Docs, c.Deprecation, c.Availability, c.Examples, c.Unmodeled = ir.Docs{}, nil, nil, nil, nil
	return f, reflect.ValueOf(c).IsZero()
}

// variantFacts returns what v says of its value, and whether it says nothing
// else: no name a source declared, no construct kept but vendor extensions, no
// nullable reference, no wire tag or number, XML hint or event, nor a field ir
// adds later.
func variantFacts(v ir.Variant) (memberFacts, bool) {
	f := memberFacts{docs: v.Docs, deprecation: v.Deprecation, availability: v.Availability,
		examples: v.Examples, unmodeled: v.Unmodeled}
	if !declaresNoName(v.Name) || !extensionsOnly(v.Unmodeled) {
		return f, false
	}
	v.Name, v.Type.Target = ir.Naming{}, ""
	v.Docs, v.Deprecation, v.Availability, v.Examples, v.Unmodeled = ir.Docs{}, nil, nil, nil, nil
	return f, reflect.ValueOf(v).IsZero()
}

// declaresNoName reports whether n names nothing a source declared, only the
// hint a position gives an anonymous type or a bare branch.
func declaresNoName(n ir.Naming) bool {
	return n.Source == "" && n.Canonical == "" && len(n.Aliases) == 0
}

// extensionsOnly reports whether every construct u keeps is a vendor
// extension, which says something of a value a member can say too. One kept
// for another reason, a constraint a Literal has no home for say, narrows the
// type holding it, which a member holding it would not.
func extensionsOnly(u ir.Unmodeled) bool {
	for _, entry := range u {
		if entry.Reason != ir.ReasonVendorExtension {
			return false
		}
	}
	return true
}

// memberFacts are what an EnumMember holds of its value beside the value and
// its name, which a variant and the type it names may say of it too.
type memberFacts struct {
	docs         ir.Docs
	deprecation  *ir.Deprecation
	availability *ir.Availability
	examples     []ir.Example
	unmodeled    ir.Unmodeled
}

// factsOf returns what m holds of its value.
func factsOf(m ir.EnumMember) memberFacts {
	return memberFacts{docs: m.Docs, deprecation: m.Deprecation, availability: m.Availability,
		examples: m.Examples, unmodeled: m.Unmodeled}
}

// none reports whether f says nothing.
func (f memberFacts) none() bool { return reflect.ValueOf(f).IsZero() }

// onto returns m holding f.
func (f memberFacts) onto(m ir.EnumMember) ir.EnumMember {
	m.Docs, m.Deprecation, m.Availability, m.Examples, m.Unmodeled =
		f.docs, f.deprecation, f.availability, f.examples, f.unmodeled
	return m
}

// with returns f joined with g, and whether no fact is said by both: a member
// holds one of each, so choosing between two would drop one.
func (f memberFacts) with(g memberFacts) (memberFacts, bool) {
	var ok [5]bool
	f.docs, ok[0] = either(f.docs, g.docs)
	f.deprecation, ok[1] = either(f.deprecation, g.deprecation)
	f.availability, ok[2] = either(f.availability, g.availability)
	f.examples, ok[3] = either(f.examples, g.examples)
	f.unmodeled, ok[4] = either(f.unmodeled, g.unmodeled)
	return f, !slices.Contains(ok[:], false)
}

// either returns whichever of a and b is set, and false where both are.
func either[T any](a, b T) (T, bool) {
	switch {
	case reflect.ValueOf(&b).Elem().IsZero():
		return a, true
	case reflect.ValueOf(&a).Elem().IsZero():
		return b, true
	default:
		return a, false
	}
}
