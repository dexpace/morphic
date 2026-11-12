// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"maps"

	"github.com/dexpace/morphic/ir"
)

// values is what a union read as an Enum holds of its values, before any Enum
// is built: the parts it is made of in order, so a union taking a branch of
// several values holds that branch's values by reference and never copies them.
type values struct {
	// common is the union's common part, which its Enum keeps and a union naming
	// it as one value reads.
	common ir.TypeCommon
	prim   ir.PrimKind
	parts  []part
	count  int
	// keys holds the key of every value, which only the one union that took
	// these values may extend.
	keys map[string]struct{}
}

// part is one member of an Enum, or the values of a branch the union took.
type part struct {
	// member is the member, before it has an ID, where taken is nil.
	member ir.EnumMember
	key    string
	// taken holds the values of the branch at id.
	taken *values
	id    ir.TypeID
}

// held is what a variant's type holds of values: one, as a part, or several,
// in a values of their own.
type held struct {
	prim   ir.PrimKind
	common ir.TypeCommon
	one    part
	set    *values
}

// held returns v as a variant's type holds it. A union of one value holds a
// member part, which every union naming it copies at constant cost.
func (v *values) held() held {
	if v.count == 1 {
		return held{prim: v.prim, common: v.common, one: v.parts[0]}
	}
	return held{prim: v.prim, common: v.common, set: v}
}

// size returns how many values p holds.
func (p part) size() int {
	if p.taken == nil {
		return 1
	}
	return p.taken.count
}

// members returns the members of the Enum root becomes, each under root's ID in
// the order of the values, and whether every one could have an ID there. A
// branch's values belong to the one union that took it, so each part is
// visited once; the stack of parts left in each is explicit, as a chain of
// unions can be as long as a document.
func (v *values) members(root ir.TypeID) ([]ir.EnumMember, bool) {
	out := make([]ir.EnumMember, 0, v.count)
	sound := true
	stack := [][]part{v.parts}
	for len(stack) > 0 {
		top := len(stack) - 1
		if len(stack[top]) == 0 {
			stack = stack[:top]
			continue
		}
		p := stack[top][0]
		stack[top] = stack[top][1:]
		if p.taken != nil {
			stack = append(stack, p.taken.parts)
			continue
		}
		id, ok := ir.EnumMemberIDFor(root, p.key, 1 /* occurrence */)
		sound = sound && ok
		p.member.ID = id
		out = append(out, p.member)
	}
	return out, sound
}

// largestSet returns the index of the part holding the most keys among those
// that took a branch, or -1 where none did.
func largestSet(parts []part) int {
	largest := -1
	for i, p := range parts {
		if p.taken != nil && (largest < 0 || len(p.taken.keys) > len(parts[largest].taken.keys)) {
			largest = i
		}
	}
	return largest
}

// mergeKeys returns the keys of every value parts hold, how many it added to a
// set, and false where two values share a key. It writes nothing before it knows
// they do not: it takes over the largest key set a part holds, which the union
// that took the part owns, and adds the keys of every other part to it, so a
// chain of unions adds each key once and not once for each union above it.
func mergeKeys(parts []part) (map[string]struct{}, int, bool) {
	largest := largestSet(parts)
	var base map[string]struct{}
	if largest >= 0 {
		base = parts[largest].taken.keys
	}

	fresh := make(map[string]struct{}, len(parts))
	add := func(key string) bool {
		_, inBase := base[key]
		_, inFresh := fresh[key]
		fresh[key] = struct{}{}
		return !inBase && !inFresh
	}
	for i, p := range parts {
		switch {
		case p.taken == nil:
			if !add(p.key) {
				return nil, 0, false
			}
		case i != largest:
			for key := range p.taken.keys {
				if !add(key) {
					return nil, 0, false
				}
			}
		}
	}
	if base == nil {
		return fresh, len(fresh), true
	}
	maps.Copy(base, fresh)
	return base, len(fresh), true
}
