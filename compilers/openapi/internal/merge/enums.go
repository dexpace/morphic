// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

// enums relates two enums alike but for their members and TypeCommon by the
// values they hold: none shared is disjoint, and otherwise verdict judges the
// members' values, the one whose values the other holds all of saying all the
// other does, beside what else each says. Two holding one set of values, or
// saying a shared value's member differently (sharedMembers), are left
// unrelated, for classify to judge by their kind, as are two differing in
// anything else.
func (j typeJudge) enums(a, b *ir.Enum) relation {
	as, bs := *a, *b
	as.TypeCommon, bs.TypeCommon, as.Members, bs.Members = ir.TypeCommon{}, ir.TypeCommon{}, nil, nil
	if !reflect.DeepEqual(as, bs) {
		return relUnrelated
	}
	aWithin, shared, aok := sharedMembers(a.Members, b.Members)
	bWithin, _, bok := sharedMembers(b.Members, a.Members)
	switch {
	case !aok || !bok:
		return relUnrelated
	case shared == 0:
		return relDisjoint
	case aWithin && bWithin:
		return relUnrelated
	}
	detail, adoptable := j.detailOf(extras{Common: a.TypeCommon}, extras{Common: b.TypeCommon})
	return verdict(holding{a: aWithin, b: bWithin}, detail, adoptable)
}

// sharedMembers counts the members of xs whose value ys holds, and reports
// whether ys holds every one. It is false where a member of ys holding one of
// those values says anything else of it otherwise, its ID aside.
func sharedMembers(xs, ys []ir.EnumMember) (within bool, shared int, ok bool) {
	within = true
	for _, x := range xs {
		y, found := memberOf(ys, x.Value)
		if !found {
			within = false
			continue
		}
		x.ID, y.ID, y.Value = "", "", x.Value
		if !reflect.DeepEqual(x, y) {
			return false, 0, false
		}
		shared++
	}
	return within, shared, true
}

// memberOf returns the member of ms holding v (sameValue).
func memberOf(ms []ir.EnumMember, v ir.Value) (ir.EnumMember, bool) {
	for _, m := range ms {
		if sameValue(m.Value, v) {
			return m, true
		}
	}
	return ir.EnumMember{}, false
}

// literalBesideDetail relates lit, a literal a Scalar node admits, against
// that node where it holds more than saysOnlyFormat allows: the literal is
// narrower where it says all the node says beside its values too, and the two
// meet where a literal can hold what the node adds (verdict). A node saying more
// of its values than a held format and bounds overlaps the literal, as before.
func (j typeJudge) literalBesideDetail(lit *ir.Literal, node ir.TypeDef) relation {
	s, ok := node.(*ir.Scalar)
	if !ok {
		return relOverlap
	}
	bare := *s
	bare.TypeCommon = ir.TypeCommon{}
	if !scalarSaysOnlyFormat(&bare) {
		return relOverlap
	}
	detail, adoptable := j.detailOf(extras{Common: lit.TypeCommon}, extras{Common: s.TypeCommon})
	return verdict(holding{a: true}, detail, adoptable)
}

// bothEnums returns a and b as enums, and false where either is not one.
func bothEnums(a, b ir.TypeDef) (*ir.Enum, *ir.Enum, bool) {
	ae, aok := a.(*ir.Enum)
	be, bok := b.(*ir.Enum)
	return ae, be, aok && bok
}

// differentLiterals reports whether a and b are two literals of different
// values (sameValue), which no value but a null both admit satisfies.
func differentLiterals(a, b ir.TypeDef) bool {
	al, aok := a.(*ir.Literal)
	bl, bok := b.(*ir.Literal)
	return aok && bok && !sameValue(al.Value, bl.Value)
}
