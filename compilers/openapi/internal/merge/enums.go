// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"reflect"

	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/ir"
)

// enums relates two enums alike but for their members, value types and
// TypeCommon by the values they hold: none shared is disjoint, and otherwise
// verdict judges the values with the value types (valueTypeHolding) beside what
// else each says. Two listing shared values in different orders hold no enum of
// both, whose order would be one's (inOneOrder). Two of value types neither
// within the other, of one set of values of one value type or in different
// orders, or saying a shared value's member differently (sharedMembers) are left
// unrelated, for classify to judge by kind, as is any other pair.
func (j typeJudge) enums(a, b *ir.Enum) relation {
	as, bs := *a, *b
	as.TypeCommon, bs.TypeCommon, as.Members, bs.Members = ir.TypeCommon{}, ir.TypeCommon{}, nil, nil
	as.ValueType, bs.ValueType = "", ""
	if !reflect.DeepEqual(as, bs) {
		return relUnrelated
	}
	ai, bi := indexMembers(a.Members), indexMembers(b.Members)
	aWithin, shared, aok := sharedMembers(a.Members, bi)
	bWithin, _, bok := sharedMembers(b.Members, ai)
	kinds, related := valueTypeHolding(a.ValueType, b.ValueType)
	ordered := inOneOrder(a.Members, bi)
	switch {
	case !aok || !bok:
		return relUnrelated
	case shared == 0:
		return relDisjoint
	case !related, aWithin && bWithin && (kinds == holdsBoth || !ordered):
		return relUnrelated
	}
	detail, adoptable := j.detailOf(extras{Common: a.TypeCommon}, extras{Common: b.TypeCommon})
	return verdict(holding{a: aWithin, b: bWithin}.and(kinds), detail, adoptable && ordered)
}

// valueTypeHolding is the holding of two enums' value types: the narrower says
// all the other does (spelling.Relate), and one says all the other does of
// itself. It is false where neither is within the other, since no value type
// says both.
func valueTypeHolding(a, b ir.PrimKind) (holding, bool) {
	return holdingOf(fromSpelling(spelling.Relate(a, b)))
}

// narrowestValueType returns the value type within every other of the enums
// nodes holds, and false where no one is, or a node is no enum.
func narrowestValueType(nodes []ir.TypeDef) (ir.PrimKind, bool) {
	var narrowest ir.PrimKind
	for k, td := range nodes {
		e, ok := td.(*ir.Enum)
		if !ok {
			return "", false
		}
		switch rel := spelling.Relate(e.ValueType, narrowest); {
		case k == 0, rel == spelling.ANarrower:
			narrowest = e.ValueType
		case rel != spelling.Same && rel != spelling.BNarrower:
			return "", false
		}
	}
	return narrowest, true
}

// sharedMembers counts the members of xs whose value ys holds, and reports
// whether ys holds every one. It is false where a member of ys holding one of
// those values says anything else of it otherwise, its ID aside.
func sharedMembers(xs []ir.EnumMember, ys memberIndex) (within bool, shared int, ok bool) {
	within = true
	for _, x := range xs {
		k, found := ys.find(x.Value)
		if !found {
			within = false
			continue
		}
		y := ys.members[k]
		x.ID, y.ID, y.Value = "", "", x.Value
		if !reflect.DeepEqual(x, y) {
			return false, 0, false
		}
		shared++
	}
	return within, shared, true
}

// inOneOrder reports whether ys lists the values it shares with xs in the order
// xs lists them.
func inOneOrder(xs []ir.EnumMember, ys memberIndex) bool {
	last := -1
	for _, x := range xs {
		k, found := ys.find(x.Value)
		if !found {
			continue
		}
		if k < last {
			return false
		}
		last = k
	}
	return true
}

// memberIndex finds an enum's first member holding a value (sameValue) without
// scanning them all, so comparing two enums costs their width, not its square.
// A value with a canonical spelling equals only one spelled alike, so such
// members are found by it, and only the rest, which sameValue compares deeply,
// are scanned.
type memberIndex struct {
	members []ir.EnumMember
	// byValue maps each canonical spelling to its first member's position.
	byValue map[string]int
	// rest lists, in order, the positions of members no canonical spelling names.
	rest []int
}

// indexMembers indexes ms by value.
func indexMembers(ms []ir.EnumMember) memberIndex {
	x := memberIndex{members: ms, byValue: make(map[string]int, len(ms))}
	for i, m := range ms {
		key, ok := canonicalValue(m.Value, 0)
		if !ok {
			x.rest = append(x.rest, i)
			continue
		}
		if _, seen := x.byValue[key]; !seen {
			x.byValue[key] = i
		}
	}
	return x
}

// find returns the position of the first member holding v, and false where
// none does.
func (x memberIndex) find(v ir.Value) (int, bool) {
	if key, ok := canonicalValue(v, 0); ok {
		i, found := x.byValue[key]
		return i, found
	}
	for _, i := range x.rest {
		if reflect.DeepEqual(x.members[i].Value, v) {
			return i, true
		}
	}
	return 0, false
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
