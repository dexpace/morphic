// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"reflect"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// constEnum returns the closed Enum u is, where u is a closed union saying
// nothing an Enum cannot (plainUnion) whose every variant names one string
// (constValue), no value twice and all of one ValueType, and false otherwise.
// The Enum keeps u's TypeCommon, so every reference to u reads it, and gives
// each member the ID its value gives it beneath u, which needs u's namespace
// among spaces, those declared for members. Its values being distinct, an
// instance matches one variant at most, so u's Exclusive says nothing lost.
// typeAt reads the type a variant names.
func constEnum(typeAt func(ir.TypeID) ir.TypeDef, spaces []string, u *ir.Union) (*ir.Enum, bool) {
	space, spaced := ir.IDSpace(ir.IDKindType, string(u.ID))
	if len(u.Variants) == 0 || !u.Closed || !plainUnion(*u) || !spaced || !slices.Contains(spaces, space) {
		return nil, false
	}
	members := make([]ir.EnumMember, 0, len(u.Variants))
	var valueType ir.PrimKind
	for i, v := range u.Variants {
		member, prim, ok := constValue(typeAt, v)
		if !ok || (i > 0 && prim != valueType) {
			return nil, false
		}
		valueType = prim
		members = append(members, member)
	}

	placed := make(map[ir.EnumMemberID]bool, len(members))
	for i := range members {
		key, keyed := ir.EnumMemberKey(members[i].Value)
		id, ok := ir.EnumMemberIDFor(u.ID, key, 1 /* occurrence */)
		if !keyed || !ok || placed[id] {
			return nil, false
		}
		placed[id] = true
		members[i].ID = id
	}
	return &ir.Enum{TypeCommon: u.TypeCommon, ValueType: valueType, Members: members, Closed: true}, true
}

// plainUnion reports whether u says nothing beside its common part, its
// variants, whether it is closed and whether it is exclusive: no tag, no
// discriminator, no input-only identity, nor a field ir adds later, none of
// which an Enum holds.
func plainUnion(u ir.Union) bool {
	u.TypeCommon, u.Variants, u.Exclusive, u.Closed = ir.TypeCommon{}, nil, false, false
	return reflect.ValueOf(u).IsZero()
}

// constValue returns the member holding the one value the type v names holds,
// with what v, that type and its member say of the value, and the primitive
// typing it, where v says nothing but that (variantFacts) and the type is an
// anonymous string Literal or closed Enum of one string saying nothing else
// (typeValue, commonFacts). A type holding several values is a set of its own,
// which the union keeps: an Enum would copy the set into each union naming it,
// which a document can make grow with the square of its size.
func constValue(typeAt func(ir.TypeID) ir.TypeDef, v ir.Variant) (ir.EnumMember, ir.PrimKind, bool) {
	said, plain := variantFacts(v)
	if !plain {
		return ir.EnumMember{}, "", false
	}
	member, prim, common, ok := typeValue(typeAt(v.Type.Target))
	if !ok {
		return ir.EnumMember{}, "", false
	}
	typed, plain := commonFacts(common)
	if !plain {
		return ir.EnumMember{}, "", false
	}
	said, ok = said.with(typed)
	if !ok {
		return ir.EnumMember{}, "", false
	}
	said, ok = said.with(factsOf(member))
	return said.onto(member), prim, ok
}

// typeValue returns the one value td holds as a member, its primitive and td's
// common part, where td is a string Literal or a closed Enum of one string,
// typed by a primitive, saying nothing else. A Literal's member is named by its
// value, as an enum's members are; an Enum's is a copy of its own.
func typeValue(td ir.TypeDef) (ir.EnumMember, ir.PrimKind, ir.TypeCommon, bool) {
	switch t := td.(type) {
	case *ir.Literal:
		if t == nil || t.Value.Kind != ir.ValueString || t.ValueType == "" {
			return ir.EnumMember{}, "", ir.TypeCommon{}, false
		}
		rest := *t
		rest.TypeCommon, rest.Value, rest.ValueType = ir.TypeCommon{}, ir.Value{}, ""
		return ir.EnumMember{Name: ir.NamingFor(t.Value.Str), Value: t.Value}, t.ValueType, t.TypeCommon,
			reflect.ValueOf(rest).IsZero()
	case *ir.Enum:
		if t == nil || !t.Closed || len(t.Members) != 1 || t.ValueType == "" ||
			t.Members[0].Value.Kind != ir.ValueString {
			return ir.EnumMember{}, "", ir.TypeCommon{}, false
		}
		rest := *t
		rest.TypeCommon, rest.ValueType, rest.Members, rest.Closed = ir.TypeCommon{}, "", nil, false
		return t.Members[0], t.ValueType, t.TypeCommon, reflect.ValueOf(rest).IsZero()
	default:
		return ir.EnumMember{}, "", ir.TypeCommon{}, false
	}
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
