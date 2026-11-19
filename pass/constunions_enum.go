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
	prim  ir.PrimitiveKind
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

// valuesOf returns the values union holds, where union is a closed union saying
// nothing an Enum cannot (plainUnion) whose every variant names values a member
// can hold (appendValues), no value twice and all of one ValueType, and false
// otherwise. Its members take IDs beneath union, which needs union's namespace
// among spaces, those declared for members. Its values being distinct, an
// instance matches one variant at most, so union's Exclusive says nothing lost.
func (r *unionReading) valuesOf(union *ir.Union) (*unionValues, bool) {
	space, spaced := ir.IDSpace(ir.IDKindType, string(union.ID))
	if len(union.Variants) == 0 || !union.Closed || !plainUnion(*union) || !spaced || !slices.Contains(r.spaces, space) {
		return nil, false
	}
	vs := &unionValues{union: union, parts: make([]valuePart, 0, len(union.Variants))}
	for i, variant := range union.Variants {
		parts, prim, ok := r.appendValues(vs.parts, variant)
		if !ok || (i > 0 && prim != vs.prim) {
			return nil, false
		}
		vs.parts, vs.prim = parts, prim
	}
	return vs, r.distinct(vs)
}

// plainUnion reports whether union says nothing beside its common part, its
// variants, whether it is closed and whether it is exclusive: no tag, no
// discriminator, no input-only identity, nor a field ir adds later, none of
// which an Enum holds.
func plainUnion(union ir.Union) bool {
	union.TypeCommon, union.Variants, union.Exclusive, union.Closed = ir.TypeCommon{}, nil, false, false
	return reflect.ValueOf(union).IsZero()
}

// appendValues appends to parts the values variant names, and returns them with
// the primitive typing them, where variant says nothing but what it says of a
// value (variantFacts) and names an anonymous type of strings saying nothing
// else (branchValues, commonFacts). One value takes what variant and its type
// say of it. Several are a set of their own, which branchValues gives only
// where variant is their type's one reference, taken where neither says
// anything of them all.
func (r *unionReading) appendValues(parts []valuePart, variant ir.Variant) ([]valuePart, ir.PrimitiveKind, bool) {
	said, plain := variantFacts(variant)
	if !plain {
		return parts, "", false
	}
	values, prim, common, ok := r.branchValues(variant.Type.Target)
	if !ok {
		return parts, "", false
	}
	typed, plain := commonFacts(common)
	if !plain {
		return parts, "", false
	}
	if len(values) > 1 || values[0].of != nil {
		if !said.none() || !typed.none() {
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
// several the values it holds. A type of several values gives them only to
// the variant that is its one reference (typeRefs.sole).
func (r *unionReading) branchValues(id ir.TypeID) ([]valuePart, ir.PrimitiveKind, ir.TypeCommon, bool) {
	switch t := r.types[id].(type) {
	case *ir.Literal:
		if t == nil || t.Value.Kind != ir.ValueString || t.ValueType == "" {
			return nil, "", ir.TypeCommon{}, false
		}
		rest := *t
		rest.TypeCommon, rest.Value, rest.ValueType = ir.TypeCommon{}, ir.Value{}, ""
		one := ir.EnumMember{Name: ir.NamingFor(t.Value.String), Value: t.Value}
		return []valuePart{{key: valueKey(t.Value), member: one}}, t.ValueType, t.TypeCommon, reflect.ValueOf(rest).IsZero()
	case *ir.Enum:
		if t == nil || !t.Closed || len(t.Members) == 0 || t.ValueType == "" ||
			(len(t.Members) > 1 && !r.refs.sole(id)) ||
			slices.ContainsFunc(t.Members, func(enumMember ir.EnumMember) bool { return enumMember.Value.Kind != ir.ValueString }) {
			return nil, "", ir.TypeCommon{}, false
		}
		r.work += len(t.Members)
		rest := *t
		rest.TypeCommon, rest.ValueType, rest.Members, rest.Closed = ir.TypeCommon{}, "", nil, false
		values := make([]valuePart, len(t.Members))
		for i, enumMember := range t.Members {
			values[i] = valuePart{key: valueKey(enumMember.Value), member: enumMember}
		}
		return values, t.ValueType, t.TypeCommon, reflect.ValueOf(rest).IsZero()
	case *ir.Union:
		vs, read := r.values[id]
		switch {
		case !read:
			return nil, "", ir.TypeCommon{}, false
		case vs.n == 1:
			return vs.parts, vs.prim, vs.union.TypeCommon, true
		case !r.refs.sole(id):
			return nil, "", ir.TypeCommon{}, false
		default:
			return []valuePart{{of: vs}}, vs.prim, vs.union.TypeCommon, true
		}
	default:
		return nil, "", ir.TypeCommon{}, false
	}
}

// valueKey returns the key of the string value (ir.EnumMemberKey), which every
// string has.
func valueKey(value ir.Value) string {
	key, _ := ir.EnumMemberKey(value)
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
	for _, valuePart := range vs.parts {
		if valuePart.of != nil && (largest == nil || valuePart.of.n > largest.n) {
			largest = valuePart.of
		}
	}
	keys := map[string]bool{}
	if largest != nil {
		keys = largest.keys
	}
	for _, valuePart := range vs.parts {
		if !r.addPart(keys, valuePart, largest) {
			return false
		}
	}
	vs.keys, vs.n = keys, len(keys)
	return true
}

// addPart adds to keys the key of each value valuePartValue holds, bar those of
// taken, whose keys keys holds already, and reports whether none was there.
func (r *unionReading) addPart(keys map[string]bool, valuePartValue valuePart, taken *unionValues) bool {
	switch valuePartValue.of {
	case nil:
		return r.addKey(keys, valuePartValue.key)
	case taken:
		return true
	}
	for key := range valuePartValue.of.keys {
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
		valuePart := top.vs.parts[top.next]
		top.next++
		if valuePart.of != nil {
			stack = append(stack, frame{vs: valuePart.of})
			continue
		}
		r.work++
		enumMember := valuePart.member
		enumMember.ID, _ = ir.EnumMemberIDFor(vs.union.ID, valuePart.key, 1 /* occurrence */)
		members = append(members, enumMember)
	}
	return &ir.Enum{TypeCommon: vs.union.TypeCommon, ValueType: vs.prim, Members: members, Closed: true}
}

// commonFacts returns what typeCommon, the common part of a variant's type,
// says of a value, and whether it says nothing else: the type is anonymous,
// named only by the hint its position gives it, keeps no construct but vendor
// extensions, and no heuristic inferred it, whose mark no member could carry.
// Its provenance goes with the type.
func commonFacts(typeCommon ir.TypeCommon) (memberFacts, bool) {
	memberFactsValue := memberFacts{docs: typeCommon.Docs, deprecation: typeCommon.Deprecation, availability: typeCommon.Availability,
		examples: typeCommon.Examples, unmodeled: typeCommon.Unmodeled}
	if !typeCommon.Anonymous || !declaresNoName(typeCommon.Name) || !extensionsOnly(typeCommon.Unmodeled) || typeCommon.Provenance.Inferred != "" {
		return memberFactsValue, false
	}
	typeCommon.ID, typeCommon.Anonymous, typeCommon.Name, typeCommon.Provenance = "", false, ir.Naming{}, ir.Provenance{}
	typeCommon.Docs, typeCommon.Deprecation, typeCommon.Availability, typeCommon.Examples, typeCommon.Unmodeled = ir.Docs{}, nil, nil, nil, nil
	return memberFactsValue, reflect.ValueOf(typeCommon).IsZero()
}

// variantFacts returns what variant says of its value, and whether it says
// nothing else: no name a source declared, no construct kept but vendor
// extensions, no nullable reference, no wire tag or number, XML hint or event,
// nor a field ir adds later.
func variantFacts(variant ir.Variant) (memberFacts, bool) {
	memberFactsValue := memberFacts{docs: variant.Docs, deprecation: variant.Deprecation, availability: variant.Availability,
		examples: variant.Examples, unmodeled: variant.Unmodeled}
	if !declaresNoName(variant.Name) || !extensionsOnly(variant.Unmodeled) {
		return memberFactsValue, false
	}
	variant.Name, variant.Type.Target = ir.Naming{}, ""
	variant.Docs, variant.Deprecation, variant.Availability, variant.Examples, variant.Unmodeled = ir.Docs{}, nil, nil, nil, nil
	return memberFactsValue, reflect.ValueOf(variant).IsZero()
}

// declaresNoName reports whether naming names nothing a source declared, only
// the hint a position gives an anonymous type or a bare branch.
func declaresNoName(naming ir.Naming) bool {
	return naming.Source == "" && naming.Canonical == "" && len(naming.Aliases) == 0
}

// extensionsOnly reports whether every construct unmodeled keeps is a vendor
// extension, which says something of a value a member can say too. One kept
// for another reason, a constraint a Literal has no home for say, narrows the
// type holding it, which a member holding it would not.
func extensionsOnly(unmodeled ir.Unmodeled) bool {
	for _, entry := range unmodeled {
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

// factsOf returns what enumMember holds of its value.
func factsOf(enumMember ir.EnumMember) memberFacts {
	return memberFacts{docs: enumMember.Docs, deprecation: enumMember.Deprecation, availability: enumMember.Availability,
		examples: enumMember.Examples, unmodeled: enumMember.Unmodeled}
}

// none reports whether f says nothing.
func (f memberFacts) none() bool { return reflect.ValueOf(f).IsZero() }

// onto returns enumMember holding f.
func (f memberFacts) onto(enumMember ir.EnumMember) ir.EnumMember {
	enumMember.Docs, enumMember.Deprecation, enumMember.Availability, enumMember.Examples, enumMember.Unmodeled =
		f.docs, f.deprecation, f.availability, f.examples, f.unmodeled
	return enumMember
}

// with returns f joined with memberFactsValue, and whether no fact is said by
// both: a member holds one of each, so choosing between two would drop one.
func (f memberFacts) with(memberFactsValue memberFacts) (memberFacts, bool) {
	var ok [5]bool
	f.docs, ok[0] = either(f.docs, memberFactsValue.docs)
	f.deprecation, ok[1] = either(f.deprecation, memberFactsValue.deprecation)
	f.availability, ok[2] = either(f.availability, memberFactsValue.availability)
	f.examples, ok[3] = either(f.examples, memberFactsValue.examples)
	f.unmodeled, ok[4] = either(f.unmodeled, memberFactsValue.unmodeled)
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
