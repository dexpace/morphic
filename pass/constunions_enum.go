// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"reflect"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// constEnum returns the closed Enum u is, where u is a closed union saying
// nothing an Enum cannot (plainUnion) whose every variant names strings
// (constValues), no value twice and all of one ValueType, and false otherwise.
// The Enum keeps u's TypeCommon, so every reference to u reads it, and gives
// each member the ID its value gives it beneath u, which needs u's namespace
// among spaces, those declared for members. Its values being distinct, an
// instance matches one variant at most, so u's Exclusive says nothing lost.
func constEnum(types ir.TypeRegistry, spaces []string, u *ir.Union) (*ir.Enum, bool) {
	space, spaced := ir.IDSpace(ir.IDKindType, string(u.ID))
	if len(u.Variants) == 0 || !u.Closed || !plainUnion(*u) || !spaced || !slices.Contains(spaces, space) {
		return nil, false
	}
	var members []ir.EnumMember
	var valueType ir.PrimKind
	for i, v := range u.Variants {
		held, prim, ok := constValues(types, v)
		if !ok || (i > 0 && prim != valueType) {
			return nil, false
		}
		valueType = prim
		members = append(members, held...)
	}

	placed := make(map[ir.EnumMemberID]bool, len(members))
	for i := range members {
		key, keyed := ir.EnumMemberKey(members[i].Value)
		id, ok := ir.EnumMemberIDFor(u.ID, key, 1)
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

// constValues returns the members the type v names holds, holding what v and
// that type say of a value, and the primitive typing them, where v says
// nothing but that (variantFacts) and the type is an anonymous string Literal
// or closed Enum of strings saying nothing else (typeValues, commonFacts). A
// type holding one value gives its member what it and v say of it; one holding
// several may say nothing of a value, as none of its members is what it
// describes.
func constValues(types ir.TypeRegistry, v ir.Variant) ([]ir.EnumMember, ir.PrimKind, bool) {
	said, plain := variantFacts(v)
	if !plain {
		return nil, "", false
	}
	members, prim, common, ok := typeValues(types[v.Type.Target])
	if !ok {
		return nil, "", false
	}
	typed, plain := commonFacts(common)
	if !plain {
		return nil, "", false
	}
	said, ok = said.with(typed)
	switch {
	case !ok:
		return nil, "", false
	case len(members) > 1:
		return members, prim, reflect.ValueOf(said).IsZero()
	}
	said, ok = said.with(factsOf(members[0]))
	members[0] = said.onto(members[0])
	return members, prim, ok
}

// typeValues returns the values td holds as members, their primitive and td's
// common part, where td is a string Literal or a closed Enum of strings, typed
// by a primitive, saying nothing else. A Literal's member is named by its
// value, as an enum's members are; an Enum's are copies of its own.
func typeValues(td ir.TypeDef) ([]ir.EnumMember, ir.PrimKind, ir.TypeCommon, bool) {
	switch t := td.(type) {
	case *ir.Literal:
		if t == nil || t.Value.Kind != ir.ValueString || t.ValueType == "" {
			return nil, "", ir.TypeCommon{}, false
		}
		rest := *t
		rest.TypeCommon, rest.Value, rest.ValueType = ir.TypeCommon{}, ir.Value{}, ""
		return []ir.EnumMember{{Name: ir.NamingFor(t.Value.Str), Value: t.Value}}, t.ValueType, t.TypeCommon,
			reflect.ValueOf(rest).IsZero()
	case *ir.Enum:
		if t == nil || !t.Closed || len(t.Members) == 0 || t.ValueType == "" ||
			slices.ContainsFunc(t.Members, func(m ir.EnumMember) bool { return m.Value.Kind != ir.ValueString }) {
			return nil, "", ir.TypeCommon{}, false
		}
		rest := *t
		rest.TypeCommon, rest.ValueType, rest.Members, rest.Closed = ir.TypeCommon{}, "", nil, false
		return slices.Clone(t.Members), t.ValueType, t.TypeCommon, reflect.ValueOf(rest).IsZero()
	default:
		return nil, "", ir.TypeCommon{}, false
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
