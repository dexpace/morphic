// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"reflect"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// constValues returns the values u is, where u is a closed union saying nothing
// an Enum cannot (plainUnion) whose every variant names string values
// (variantPart), all of one ValueType and none twice, and false otherwise. The
// Enum u becomes keeps u's TypeCommon, so every reference to u reads it, and
// gives each member the ID its value gives it beneath u, which needs u's
// namespace among spaces, those declared for members. Its values being
// distinct, an instance matches one variant at most, so u's Exclusive says
// nothing lost.
func (r *unionReading) constValues(u *ir.Union) (*values, bool) {
	space, spaced := ir.IDSpace(ir.IDKindType, string(u.ID))
	if len(u.Variants) == 0 || !u.Closed || !plainUnion(*u) || !spaced || !slices.Contains(r.spaces, space) {
		return nil, false
	}
	parts := make([]part, 0, len(u.Variants))
	var valueType ir.PrimKind
	count := 0
	for i, v := range u.Variants {
		p, prim, ok := r.variantPart(v)
		if !ok || (i > 0 && prim != valueType) {
			return nil, false
		}
		valueType = prim
		parts = append(parts, p)
		count += p.size()
	}

	keys, moved, distinct := mergeKeys(parts)
	if !distinct {
		return nil, false
	}
	r.moved += moved
	return &values{common: u.TypeCommon, prim: valueType, parts: parts, count: count, keys: keys}, true
}

// plainUnion reports whether u says nothing beside its common part, its
// variants, whether it is closed and whether it is exclusive: no tag, no
// discriminator, no input-only identity, nor a field ir adds later, none of
// which an Enum holds.
func plainUnion(u ir.Union) bool {
	u.TypeCommon, u.Variants, u.Exclusive, u.Closed = ir.TypeCommon{}, nil, false, false
	return reflect.ValueOf(u).IsZero()
}

// variantPart returns the part the type v names is, with the primitive typing
// its values, where v says nothing but what a member says of its value
// (variantFacts) and the type is an anonymous string Literal or closed Enum
// saying nothing else (heldAt, commonFacts). One value is a member holding what
// v, the type and its member say of it. Several are a set of their own, taken
// whole where nothing is said of them all, which an Enum could not say of its
// values apart.
func (r *unionReading) variantPart(v ir.Variant) (part, ir.PrimKind, bool) {
	said, plain := variantFacts(v)
	if !plain {
		return part{}, "", false
	}
	h, ok := r.heldAt(v.Type.Target)
	if !ok {
		return part{}, "", false
	}
	typed, plain := commonFacts(h.common)
	if !plain {
		return part{}, "", false
	}
	if h.set != nil {
		taken := part{taken: h.set, id: v.Type.Target}
		return taken, h.prim, said.empty() && typed.empty()
	}

	said, ok = said.with(typed)
	if !ok {
		return part{}, "", false
	}
	said, ok = said.with(factsOf(h.one.member))
	one := h.one
	one.member = said.onto(one.member)
	return one, h.prim, ok
}

// heldAt returns what the type at id holds of values, and false where it holds
// none of the kind a union reads as an Enum. Several values are a set of their
// own, which only the one union naming it may take (r.sole): an Enum would copy
// it into each union naming it, which a document can make grow with the
// square of its size.
func (r *unionReading) heldAt(id ir.TypeID) (held, bool) {
	if v, read := r.values[id]; read {
		return v.held(), v.count == 1 || r.sole[id]
	}
	switch t := r.types[id].(type) {
	case *ir.Literal:
		return literalHeld(t)
	case *ir.Enum:
		return r.enumHeld(id, t)
	default:
		return held{}, false
	}
}

// literalHeld returns the one value t holds, where t is an anonymous string
// Literal typed by a primitive, saying nothing else. Its member is named by the
// value, as an enum's members are.
func literalHeld(t *ir.Literal) (held, bool) {
	if t == nil || t.Value.Kind != ir.ValueString || t.ValueType == "" {
		return held{}, false
	}
	rest := *t
	rest.TypeCommon, rest.Value, rest.ValueType = ir.TypeCommon{}, ir.Value{}, ""
	key, keyed := ir.EnumMemberKey(t.Value)
	one := part{member: ir.EnumMember{Name: ir.NamingFor(t.Value.Str), Value: t.Value}, key: key}
	return held{prim: t.ValueType, common: t.TypeCommon, one: one}, keyed && reflect.ValueOf(rest).IsZero()
}

// enumHeld returns the values the Enum t at id holds, where t is closed,
// string-valued, typed by a primitive and says nothing else, and holds one value
// or several that only a union naming it may take. A member is a copy of its
// own.
func (r *unionReading) enumHeld(id ir.TypeID, t *ir.Enum) (held, bool) {
	if t == nil || !t.Closed || len(t.Members) == 0 || t.ValueType == "" || (len(t.Members) > 1 && !r.sole[id]) {
		return held{}, false
	}
	rest := *t
	rest.TypeCommon, rest.ValueType, rest.Members, rest.Closed = ir.TypeCommon{}, "", nil, false
	if !reflect.ValueOf(rest).IsZero() {
		return held{}, false
	}

	parts := make([]part, 0, len(t.Members))
	for _, m := range t.Members {
		key, keyed := ir.EnumMemberKey(m.Value)
		if !keyed || m.Value.Kind != ir.ValueString {
			return held{}, false
		}
		parts = append(parts, part{member: m, key: key})
	}
	if len(parts) == 1 {
		return held{prim: t.ValueType, common: t.TypeCommon, one: parts[0]}, true
	}
	keys, moved, distinct := mergeKeys(parts)
	r.moved += moved
	set := &values{common: t.TypeCommon, prim: t.ValueType, parts: parts, count: len(parts), keys: keys}
	return held{prim: t.ValueType, common: t.TypeCommon, set: set}, distinct
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

// empty reports whether f says nothing of a value.
func (f memberFacts) empty() bool { return reflect.ValueOf(f).IsZero() }

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
